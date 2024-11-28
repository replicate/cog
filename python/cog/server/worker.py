import asyncio
import contextlib
import contextvars
import inspect
import multiprocessing
import os
import select
import signal
import sys
import threading
import traceback
import types
import uuid
import warnings
from concurrent.futures import Future, ThreadPoolExecutor
from enum import Enum, auto, unique
from multiprocessing.connection import Connection
from typing import (
    Any,
    Callable,
    Dict,
    Iterator,
    Optional,
    Tuple,
    Union,
    cast,
)

import structlog
from attrs import define

from ..base_predictor import BasePredictor
from ..json import make_encodeable
from ..predictor import get_predict, load_predictor_from_ref, run_setup
from ..types import PYDANTIC_V2, URLPath
from ..wait import wait_for_env
from .connection import AsyncConnection, LockedConnection
from .eventtypes import (
    Cancel,
    Done,
    Envelope,
    Log,
    PredictionInput,
    PredictionMetric,
    PredictionOutput,
    PredictionOutputType,
    Shutdown,
)
from .exceptions import (
    CancelationException,
    FatalWorkerException,
    InvalidStateException,
)
from .helpers import SimpleStreamRedirector, StreamRedirector
from .scope import Scope, scope

if PYDANTIC_V2:
    from .helpers import unwrap_pydantic_serialization_iterators

_spawn = multiprocessing.get_context("spawn")
_tag_var: contextvars.ContextVar[Optional[str]] = contextvars.ContextVar(
    "tag", default=None
)

_PublicEventType = Union[Done, Log, PredictionOutput, PredictionOutputType]

log = structlog.get_logger("cog.server.worker")


@unique
class WorkerState(Enum):
    NEW = auto()
    STARTING = auto()
    READY = auto()
    DEFUNCT = auto()


@define
class PredictionRequest:
    tag: Optional[str]


@define
class CancelRequest:
    tag: Optional[str]


@define
class PredictionState:
    tag: Optional[str]
    payload: Dict[str, Any]
    result: "Future[Done]"

    cancel_sent: bool = False


class Worker:
    def __init__(
        self, child: "_ChildWorker", events: Connection, max_concurrency: int = 1
    ) -> None:
        self._child = child
        self._events = events

        self._sent_shutdown_event = False
        self._state = WorkerState.NEW
        self._terminating = False

        self._setup_result: "Future[Done]" = Future()
        self._subscribers_lock = threading.Lock()
        self._subscribers: Dict[
            int, Tuple[Callable[[_PublicEventType], None], Optional[str]]
        ] = {}

        self._max_concurrency = max_concurrency

        self._predictions_lock = threading.Lock()
        self._predictions_in_flight: Dict[Optional[str], PredictionState] = {}

        recv_conn, send_conn = _spawn.Pipe(duplex=False)
        self._request_send_conn = send_conn
        self._request_recv_conn = recv_conn

        self._pool = ThreadPoolExecutor(max_workers=1)
        self._event_consumer = None

    def setup(self) -> "Future[Done]":
        self._assert_state(WorkerState.NEW)
        self._state = WorkerState.STARTING
        self._child.start()
        self._event_consumer = self._pool.submit(self._consume_events)
        return self._setup_result

    def predict(
        self, payload: Dict[str, Any], tag: Optional[str] = None
    ) -> "Future[Done]":
        # TODO: tag is Optional, but it's required when in concurrent mode and
        # basically unnecessary in sequential mode. Should we have a separate
        # ConcurrentWorker?
        if self._max_concurrency > 1 and tag is None:
            raise TypeError(
                "Invalid operation: tag is required when Worker has max_concurrency > 1"
            )

        with self._predictions_lock:
            if len(self._predictions_in_flight) >= self._max_concurrency:
                raise InvalidStateException(
                    "Invalid operation: maximum predictions in flight reached"
                )
            if tag in self._predictions_in_flight:
                raise InvalidStateException(
                    f"Invalid operation: prediction with tag {tag} already running"
                )
            self._assert_state(WorkerState.READY)
            result = Future()
            self._predictions_in_flight[tag] = PredictionState(tag, payload, result)
        self._request_send_conn.send(PredictionRequest(tag))
        return result

    def subscribe(
        self,
        subscriber: Callable[[_PublicEventType], None],
        tag: Optional[str] = None,
    ) -> int:
        sid = uuid.uuid4().int
        with self._subscribers_lock:
            self._subscribers[sid] = (subscriber, tag)
        return sid

    def unsubscribe(self, sid: int) -> None:
        with self._subscribers_lock:
            del self._subscribers[sid]

    def shutdown(self, timeout: Optional[float] = None) -> None:
        """
        Shut down the worker gracefully. This waits for the child worker to
        finish any in-flight work and exit.
        """
        self._terminating = True
        self._state = WorkerState.DEFUNCT

        if self._child.is_alive() and not self._sent_shutdown_event:
            self._events.send(Envelope(event=Shutdown()))
            self._sent_shutdown_event = True

        if self._event_consumer:
            self._event_consumer.result(timeout=timeout)

        self._pool.shutdown()

    def terminate(self) -> None:
        """
        Shut down the worker immediately. This may not correctly clean up
        resources used by the worker.
        """
        self._terminating = True
        self._state = WorkerState.DEFUNCT

        if self._child.is_alive():
            self._child.terminate()
            self._child.join()

        self._pool.shutdown(wait=False)

    def cancel(self, tag: Optional[str] = None) -> None:
        self._request_send_conn.send(CancelRequest(tag))

    def _assert_state(self, state: WorkerState) -> None:
        if self._state != state:
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be {state})"
            )

    def _consume_events_until_done(self) -> Optional[Done]:
        while self._child.is_alive():
            if not self._events.poll(0.1):
                continue

            e = self._events.recv()
            self._publish(e)

            if isinstance(e.event, Done):
                return e.event
        return None

    def _consume_events(self) -> None:
        try:
            self._consume_events_inner()
        except:
            log.fatal("unhandled error in _consume_events", exc_info=True)
            raise

    def _consume_events_inner(self) -> None:
        # Setup
        done = self._consume_events_until_done()
        # If we didn't get a done event, the child process died.
        if not done:
            exitcode = self._child.exitcode
            self._setup_result.set_exception(
                FatalWorkerException(
                    f"Predictor setup failed for an unknown reason. It might have run out of memory? (exitcode {exitcode})"
                )
            )
            self._state = WorkerState.DEFUNCT
            return
        if done.error:
            self._setup_result.set_exception(
                FatalWorkerException(
                    "Predictor errored during setup: " + done.error_detail
                )
            )
            self._state = WorkerState.DEFUNCT
            return

        # We capture the setup future and then set state to READY before
        # completing it, so that we can immediately accept work.
        self._state = WorkerState.READY
        self._setup_result.set_result(done)

        # Main event loop
        while self._child.is_alive():
            # see if we have any new prediction requests

            read_socks, _, _ = select.select(
                [self._request_recv_conn, self._events], [], [], 0.1
            )
            if self._request_recv_conn in read_socks:
                ev = self._request_recv_conn.recv()
                if isinstance(ev, PredictionRequest):
                    with self._predictions_lock:
                        state = self._predictions_in_flight[ev.tag]

                    # Prepare payload (download URLPath objects)
                    # FIXME this blocks the event loop, which is bad in concurrent mode
                    try:
                        _prepare_payload(state.payload)
                    except Exception as e:
                        done = Done(error=True, error_detail=str(e))
                        self._publish(Envelope(done, state.tag))
                        self._complete_prediction(done, state.tag)
                    else:
                        # Start the prediction
                        self._events.send(
                            Envelope(
                                event=PredictionInput(payload=state.payload),
                                tag=state.tag,
                            )
                        )
                elif isinstance(ev, CancelRequest):
                    with self._predictions_lock:
                        predict_state = self._predictions_in_flight.get(ev.tag)
                        if predict_state and not predict_state.cancel_sent:
                            self._child.send_cancel()
                            self._events.send(Envelope(event=Cancel(), tag=ev.tag))
                            predict_state.cancel_sent = True
                else:
                    log.warn("unrecognized request event: {ev}")

            if self._events in read_socks:
                ev = self._events.recv()
                self._publish(ev)
                if isinstance(ev.event, Done):
                    self._complete_prediction(ev.event, ev.tag)

        # If we dropped off the end off the end of the loop, it's because the
        # child process died.  First, process any remaining messages on the connection
        while self._events.poll():
            ev = self._events.recv()
            self._publish(ev)
            if isinstance(ev.event, Done):
                self._complete_prediction(ev.event, ev.tag)

        if not self._terminating:
            self._state = WorkerState.DEFUNCT
            with self._predictions_lock:
                for state in self._predictions_in_flight.values():
                    exitcode = self._child.exitcode
                    state.result.set_exception(
                        FatalWorkerException(
                            f"Prediction failed for an unknown reason. It might have run out of memory? (exitcode {exitcode})"
                        )
                    )
                self._predictions_in_flight.clear()

    def _complete_prediction(self, done: Done, tag: Optional[str]) -> None:
        # We update the in-flight dictionary before completing the prediction
        # future, so that we can immediately accept work.
        with self._predictions_lock:
            predict_state = self._predictions_in_flight.pop(tag)
        predict_state.result.set_result(done)

    def _publish(self, e: Envelope) -> None:
        with self._subscribers_lock:
            subscribers_copy = list(self._subscribers.values())
        for subscriber, requested_tag in subscribers_copy:
            if requested_tag is None or e.tag == requested_tag:
                try:
                    subscriber(cast(_PublicEventType, e.event))
                except Exception:
                    log.warn(
                        "publish failed",
                        subscriber=subscriber,
                        tag=e.tag,
                        event=e.event,
                        exc_info=True,
                    )


class _ChildWorker(_spawn.Process):  # type: ignore
    def __init__(
        self,
        predictor_ref: str,
        *,
        is_async: bool,
        events: Connection,
        max_concurrency: int = 1,
        tee_output: bool = True,
    ) -> None:
        self._predictor_ref = predictor_ref
        self._predictor: Optional[BasePredictor] = None
        self._events: Union[AsyncConnection, LockedConnection] = LockedConnection(
            events
        )
        self._tee_output = tee_output
        self._cancelable = False
        self._max_concurrency = max_concurrency

        # for synchronous predictors only! async predictors use _tag_var instead
        self._sync_tag: Optional[str] = None
        self._is_async = is_async

        super().__init__()

    def run(self) -> None:
        # If we're running at a shell, SIGINT will be sent to every process in
        # the process group. We ignore it in the child process and require that
        # shutdown is coordinated by the parent process.
        signal.signal(signal.SIGINT, signal.SIG_IGN)

        # Initially, we ignore SIGUSR1.
        signal.signal(signal.SIGUSR1, signal.SIG_IGN)

        if self._is_async:
            redirector = SimpleStreamRedirector(
                callback=self._stream_write_hook,
                tee=self._tee_output,
            )
        else:
            redirector = StreamRedirector(
                callback=self._stream_write_hook,
                tee=self._tee_output,
            )

        with scope(Scope(record_metric=self.record_metric)), redirector:
            self._predictor = self._load_predictor()

            # If _load_predictor hasn't returned a predictor instance then
            # it has sent a error Done event and we're done here.
            if not self._predictor:
                return
            self._predictor.log = self._log

            predict = get_predict(self._predictor)
            if self._is_async:
                assert isinstance(redirector, SimpleStreamRedirector)
                self._setup(redirector)
                asyncio.run(self._aloop(predict, redirector))
            else:
                # We use SIGUSR1 to signal an interrupt for cancelation.
                signal.signal(signal.SIGUSR1, self._signal_handler)

                assert isinstance(redirector, StreamRedirector)
                self._setup(redirector)
                self._loop(
                    predict,
                    redirector,
                )

    def send_cancel(self) -> None:
        if self.is_alive() and self.pid:
            os.kill(self.pid, signal.SIGUSR1)

    def record_metric(self, name: str, value: Union[float, int]) -> None:
        self._events.send(
            Envelope(PredictionMetric(name, value), tag=self._current_tag)
        )

    @property
    def _current_tag(self) -> Optional[str]:
        # if _tag_var is set, use that (only applies within _apredict())
        tag = _tag_var.get()
        if tag:
            return tag
        return self._sync_tag

    def _load_predictor(self) -> Optional[BasePredictor]:
        done = Done()
        wait_for_env()
        try:
            return load_predictor_from_ref(self._predictor_ref)
        except Exception as e:  # pylint: disable=broad-exception-caught
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
            self._events.send(Envelope(event=done))
        except BaseException as e:
            # For SystemExit and friends we attempt to add some useful context
            # to the logs, but reraise to ensure the process dies.
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
            self._events.send(Envelope(event=done))
            raise

        return None

    def _setup(
        self, redirector: Union[StreamRedirector, SimpleStreamRedirector]
    ) -> None:
        done = Done()
        try:
            assert self._predictor

            # Could be a function or a class
            if hasattr(self._predictor, "setup"):
                run_setup(self._predictor)

            predict = get_predict(self._predictor)

            is_async_predictor = inspect.iscoroutinefunction(
                predict
            ) or inspect.isasyncgenfunction(predict)

            # Async models require python >= 3.11 so we can use asyncio.TaskGroup
            # We should check for this before getting to this point
            if is_async_predictor and sys.version_info < (3, 11):
                raise FatalWorkerException(
                    "Cog requires Python >=3.11 for `async def predict()` support"
                )

            if self._max_concurrency > 1 and not is_async_predictor:
                raise FatalWorkerException(
                    "max_concurrency > 1 requires an async predict function, e.g. `async def predict()`"
                )

        except Exception as e:  # pylint: disable=broad-exception-caught
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
        except BaseException as e:
            # For SystemExit and friends we attempt to add some useful context
            # to the logs, but reraise to ensure the process dies.
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
            raise
        finally:
            try:
                redirector.drain(timeout=10)
            except TimeoutError:
                self._events.send(
                    Envelope(
                        event=Log(
                            "WARNING: logs may be truncated due to excessive volume.",
                            source="stderr",
                        )
                    )
                )
                raise
            self._events.send(Envelope(event=done))

    def _loop(
        self,
        predict: Callable[..., Any],
        redirector: StreamRedirector,
    ) -> None:
        while True:
            e = cast(Envelope, self._events.recv())
            if isinstance(e.event, Cancel):
                continue  # Ignored in sync predictors.
            elif isinstance(e.event, Shutdown):
                break
            elif isinstance(e.event, PredictionInput):
                self._predict(e.tag, e.event.payload, predict, redirector)
            else:
                print(f"Got unexpected event: {e.event}", file=sys.stderr)

    async def _aloop(
        self,
        predict: Callable[..., Any],
        redirector: SimpleStreamRedirector,
    ) -> None:
        # Unwrap and replace the events connection with an async one.
        assert isinstance(self._events, LockedConnection)
        self._events = AsyncConnection(self._events.connection)

        task = None

        async with asyncio.TaskGroup() as tg:
            while True:
                e = cast(Envelope, await self._events.recv())
                if isinstance(e.event, Cancel) and task and self._cancelable:
                    task.cancel()
                elif isinstance(e.event, Shutdown):
                    break
                elif isinstance(e.event, PredictionInput):
                    task = tg.create_task(
                        self._apredict(e.tag, e.event.payload, predict, redirector)
                    )
                else:
                    print(f"Got unexpected event: {e.event}", file=sys.stderr)

    def _predict(
        self,
        tag: Optional[str],
        payload: Dict[str, Any],
        predict: Callable[..., Any],
        redirector: StreamRedirector,
    ) -> None:
        with self._handle_predict_error(redirector, tag=tag):
            result = predict(**payload)

            if result:
                if isinstance(result, types.GeneratorType):
                    self._events.send(
                        Envelope(
                            event=PredictionOutputType(multi=True),
                            tag=tag,
                        )
                    )
                    for r in result:
                        if PYDANTIC_V2:
                            payload = make_encodeable(
                                unwrap_pydantic_serialization_iterators(r)
                            )
                        else:
                            payload = make_encodeable(r)
                        self._events.send(
                            Envelope(
                                event=PredictionOutput(payload=payload),
                                tag=tag,
                            )
                        )
                else:
                    self._events.send(
                        Envelope(
                            event=PredictionOutputType(multi=False),
                            tag=tag,
                        )
                    )
                    if PYDANTIC_V2:
                        payload = make_encodeable(
                            unwrap_pydantic_serialization_iterators(result)
                        )
                    else:
                        payload = make_encodeable(result)
                    self._events.send(
                        Envelope(
                            event=PredictionOutput(payload=payload),
                            tag=tag,
                        )
                    )

    async def _apredict(
        self,
        tag: Optional[str],
        payload: Dict[str, Any],
        predict: Callable[..., Any],
        redirector: SimpleStreamRedirector,
    ) -> None:
        _tag_var.set(tag)

        with self._handle_predict_error(redirector, tag=tag):
            future_result = predict(**payload)

            if future_result:
                if inspect.isasyncgen(future_result):
                    self._events.send(
                        Envelope(
                            event=PredictionOutputType(multi=True),
                            tag=tag,
                        )
                    )
                    async for r in future_result:
                        if PYDANTIC_V2:
                            payload = make_encodeable(
                                unwrap_pydantic_serialization_iterators(r)
                            )
                        else:
                            payload = make_encodeable(r)
                        self._events.send(
                            Envelope(
                                event=PredictionOutput(payload=payload),
                                tag=tag,
                            )
                        )
                else:
                    result = await future_result
                    self._events.send(
                        Envelope(
                            event=PredictionOutputType(multi=False),
                            tag=tag,
                        )
                    )
                    if PYDANTIC_V2:
                        payload = make_encodeable(
                            unwrap_pydantic_serialization_iterators(result)
                        )
                    else:
                        payload = make_encodeable(result)
                    self._events.send(
                        Envelope(
                            event=PredictionOutput(payload=payload),
                            tag=tag,
                        )
                    )

    @contextlib.contextmanager
    def _handle_predict_error(
        self,
        redirector: Union[SimpleStreamRedirector, StreamRedirector],
        tag: Optional[str],
    ) -> Iterator[None]:
        done = Done()
        send_done = True
        self._cancelable = True
        self._sync_tag = tag
        try:
            yield
        # regular cancelation
        except CancelationException:
            done.canceled = True
        # async cancelation
        except asyncio.CancelledError:
            done.canceled = True
            # We've handled the requested cancelation, so we uncancel the task.
            # This ensures that any cleanup work we do won't be interrupted.
            task = asyncio.current_task()
            assert task
            task.uncancel()
        except Exception as e:  # pylint: disable=broad-exception-caught
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
        except BaseException:
            # For SystemExit and friends we attempt to add some useful context
            # to the logs, but reraise to ensure the process dies.
            traceback.print_exc()
            # This is fatal, so we should not send a done event, as this
            # implies we're ready for more work.
            send_done = False
            raise
        finally:
            self._cancelable = False
            try:
                redirector.drain(timeout=10)
            except TimeoutError:
                self._events.send(
                    Envelope(
                        event=Log(
                            "WARNING: logs may be truncated due to excessive volume.",
                            source="stderr",
                        ),
                        tag=tag,
                    )
                )
                raise
            if send_done:
                self._events.send(Envelope(event=done, tag=tag))
            self._sync_tag = None

    def _signal_handler(
        self,
        signum: int,
        frame: Optional[types.FrameType],  # pylint: disable=unused-argument
    ) -> None:
        if signum == signal.SIGUSR1 and self._cancelable:
            raise CancelationException()

    def _stream_write_hook(self, stream_name: str, data: str) -> None:
        if len(data) == 0:
            return

        if stream_name == sys.stdout.name:
            self._events.send(
                Envelope(event=Log(data, source="stdout"), tag=self._current_tag)
            )
        else:
            self._events.send(
                Envelope(event=Log(data, source="stderr"), tag=self._current_tag)
            )

    def _log(self, *messages: str, source: str = "stderr") -> None:
        """
        DEPRECATED: This function will be removed in a future version of cog.
        """
        warnings.warn(
            "log() is deprecated and will be removed in a future version. Use `print` or `logging` module instead",
            category=DeprecationWarning,
            stacklevel=1,
        )
        file = sys.stdout if source == "stdout" else sys.stderr
        print(*messages, file=file, end="")


def make_worker(
    predictor_ref: str,
    *,
    is_async: bool,
    tee_output: bool = True,
    max_concurrency: int = 1,
) -> Worker:
    parent_conn, child_conn = _spawn.Pipe()
    child = _ChildWorker(
        predictor_ref,
        is_async=is_async,
        events=child_conn,
        tee_output=tee_output,
        max_concurrency=max_concurrency,
    )
    parent = Worker(child=child, events=parent_conn, max_concurrency=max_concurrency)
    return parent


def _prepare_payload(payload: Dict[str, Any]) -> None:
    for k, v in payload.items():
        # Check if v is an instance of URLPath
        if isinstance(v, URLPath):
            payload[k] = v.convert()
        # Check if v is a list of URLPath instances
        elif isinstance(v, list) and all(isinstance(item, URLPath) for item in v):
            payload[k] = [item.convert() for item in v]
