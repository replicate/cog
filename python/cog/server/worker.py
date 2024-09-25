import multiprocessing
import os
import signal
import sys
import threading
import traceback
import types
import uuid
from concurrent.futures import Future, ThreadPoolExecutor
from enum import Enum, auto, unique
from multiprocessing.connection import Connection
from typing import Any, Callable, Dict, Optional, Union

import structlog

from ..json import make_encodeable
from ..predictor import BasePredictor, get_predict, load_predictor_from_ref, run_setup
from ..types import URLPath
from .eventtypes import (
    Done,
    Log,
    PredictionInput,
    PredictionOutput,
    PredictionOutputType,
    Shutdown,
)
from .exceptions import (
    CancelationException,
    FatalWorkerException,
    InvalidStateException,
)
from .helpers import StreamRedirector

_spawn = multiprocessing.get_context("spawn")

_PublicEventType = Union[Done, Log, PredictionOutput, PredictionOutputType]

log = structlog.get_logger("cog.server.worker")


@unique
class WorkerState(Enum):
    NEW = auto()
    STARTING = auto()
    READY = auto()
    PROCESSING = auto()
    DEFUNCT = auto()


class Worker:
    def __init__(self, child: "ChildWorker", events: Connection) -> None:
        self._child = child
        self._events = events

        self._allow_cancel = False
        self._sent_shutdown_event = False
        self._state = WorkerState.NEW
        self._terminating = False

        self._result: Optional["Future[Done]"] = None
        self._subscribers: Dict[int, Callable[[_PublicEventType], None]] = {}

        self._predict_payload: Optional[Dict[str, Any]] = None
        self._predict_start = threading.Event()  # set when a prediction is started

        self._pool = ThreadPoolExecutor(max_workers=1)
        self._event_consumer = None

    def setup(self) -> "Future[Done]":
        self._assert_state(WorkerState.NEW)
        self._state = WorkerState.STARTING
        result = Future()
        self._result = result
        self._child.start()
        self._event_consumer = self._pool.submit(self._consume_events)
        return result

    def predict(self, payload: Dict[str, Any]) -> "Future[Done]":
        self._assert_state(WorkerState.READY)
        self._state = WorkerState.PROCESSING
        self._allow_cancel = True
        result = Future()
        self._result = result
        self._predict_payload = payload
        self._predict_start.set()
        return result

    def subscribe(self, subscriber: Callable[[_PublicEventType], None]) -> int:
        sid = uuid.uuid4().int
        self._subscribers[sid] = subscriber
        return sid

    def unsubscribe(self, sid: int) -> None:
        del self._subscribers[sid]

    def shutdown(self, timeout: Optional[float] = None) -> None:
        """
        Shut down the worker gracefully. This waits for the child worker to
        finish any in-flight work and exit.
        """
        self._terminating = True
        self._state = WorkerState.DEFUNCT

        if self._child.is_alive() and not self._sent_shutdown_event:
            self._events.send(Shutdown())
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

    def cancel(self) -> None:
        if self._allow_cancel:
            self._child.send_cancel()
            self._allow_cancel = False

    def _assert_state(self, state: WorkerState) -> None:
        if self._state != state:
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be {state})"
            )

    def _consume_events_until_done(self) -> Optional[Done]:
        while self._child.is_alive():
            if not self._events.poll(0.1):
                continue

            ev = self._events.recv()
            self._publish(ev)

            if isinstance(ev, Done):
                return ev
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
            assert self._result
            self._result.set_exception(
                FatalWorkerException(
                    f"Predictor setup failed for an unknown reason. It might have run out of memory? (exitcode {exitcode})"
                )
            )
            self._result = None
            self._state = WorkerState.DEFUNCT
            return
        if done.error:
            assert self._result
            self._result.set_exception(
                FatalWorkerException(
                    "Predictor errored during setup: " + done.error_detail
                )
            )
            self._result = None
            self._state = WorkerState.DEFUNCT
            return

        assert self._result

        # We capture the setup future and then set state to READY before
        # completing it, so that we can immediately accept work.
        result = self._result
        self._result = None
        self._state = WorkerState.READY
        result.set_result(done)

        # Predictions
        while self._child.is_alive():
            start = self._predict_start.wait(timeout=0.1)
            if not start:
                continue

            assert self._predict_payload is not None
            assert self._result

            # Prepare payload (download URLPath objects)
            try:
                _prepare_payload(self._predict_payload)
            except Exception as e:
                done = Done(error=True, error_detail=str(e))
                self._publish(done)
            else:
                # Start the prediction
                self._events.send(PredictionInput(payload=self._predict_payload))

                # Consume and publish prediction events
                done = self._consume_events_until_done()
                if not done:
                    break

            # We capture the predict future and then reset state before
            # completing it, so that we can immediately accept work.
            result = self._result
            self._predict_payload = None
            self._predict_start.clear()
            self._result = None
            self._state = WorkerState.READY
            self._allow_cancel = False
            result.set_result(done)

        # If we dropped off the end off the end of the loop, it's because the
        # child process died.
        if not self._terminating:
            if self._result:
                exitcode = self._child.exitcode
                self._result.set_exception(
                    FatalWorkerException(
                        f"Prediction failed for an unknown reason. It might have run out of memory? (exitcode {exitcode})"
                    )
                )
                self._result = None
            self._state = WorkerState.DEFUNCT

    def _publish(self, ev: _PublicEventType) -> None:
        for subscriber in self._subscribers.values():
            try:
                subscriber(ev)
            except Exception:
                log.warn("publish failed", subscriber=subscriber, ev=ev, exc_info=True)


class LockedConn:
    def __init__(self, conn: Connection) -> None:
        self.conn = conn
        self._lock = _spawn.Lock()

    def send(self, obj: Any) -> None:
        with self._lock:
            self.conn.send(obj)

    def recv(self) -> Any:
        return self.conn.recv()


class ChildWorker(_spawn.Process):  # type: ignore
    def __init__(
        self,
        predictor_ref: str,
        events: Connection,
        tee_output: bool = True,
    ) -> None:
        self._predictor_ref = predictor_ref
        self._predictor: Optional[BasePredictor] = None
        self._events = LockedConn(events)
        self._tee_output = tee_output
        self._cancelable = False

        super().__init__()

    def run(self) -> None:
        # If we're running at a shell, SIGINT will be sent to every process in
        # the process group. We ignore it in the child process and require that
        # shutdown is coordinated by the parent process.
        signal.signal(signal.SIGINT, signal.SIG_IGN)

        # We use SIGUSR1 to signal an interrupt for cancelation.
        signal.signal(signal.SIGUSR1, self._signal_handler)

        redirector = StreamRedirector(
            tee=self._tee_output,
            callback=self._stream_write_hook,
        )

        with redirector:
            self._setup(redirector)
            self._loop(redirector)

    def send_cancel(self) -> None:
        if self.is_alive() and self.pid:
            os.kill(self.pid, signal.SIGUSR1)

    def _setup(self, redirector: StreamRedirector) -> None:
        done = Done()
        try:
            self._predictor = load_predictor_from_ref(self._predictor_ref)
            # Could be a function or a class
            if hasattr(self._predictor, "setup"):
                run_setup(self._predictor)
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
                    Log(
                        "WARNING: logs may be truncated due to excessive volume.",
                        source="stderr",
                    )
                )
                raise
            self._events.send(done)

    def _loop(self, redirector: StreamRedirector) -> None:
        while True:
            ev = self._events.recv()
            if isinstance(ev, Shutdown):
                break
            if isinstance(ev, PredictionInput):
                self._predict(ev.payload, redirector)
            else:
                print(f"Got unexpected event: {ev}", file=sys.stderr)

    def _predict(
        self,
        payload: Dict[str, Any],
        redirector: StreamRedirector,
    ) -> None:
        assert self._predictor
        done = Done()
        send_done = True
        self._cancelable = True
        try:
            predict = get_predict(self._predictor)
            result = predict(**payload)

            if result:
                if isinstance(result, types.GeneratorType):
                    self._events.send(PredictionOutputType(multi=True))
                    for r in result:
                        self._events.send(PredictionOutput(payload=make_encodeable(r)))
                else:
                    self._events.send(PredictionOutputType(multi=False))
                    self._events.send(PredictionOutput(payload=make_encodeable(result)))
        except CancelationException:
            done.canceled = True
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
                    Log(
                        "WARNING: logs may be truncated due to excessive volume.",
                        source="stderr",
                    )
                )
                raise
            if send_done:
                self._events.send(done)

    def _signal_handler(
        self,
        signum: int,
        frame: Optional[types.FrameType],  # pylint: disable=unused-argument
    ) -> None:
        if signum == signal.SIGUSR1 and self._cancelable:
            raise CancelationException()

    def _stream_write_hook(self, stream_name: str, data: str) -> None:
        if stream_name == sys.stdout.name:
            self._events.send(Log(data, source="stdout"))
        else:
            self._events.send(Log(data, source="stderr"))


def make_worker(predictor_ref: str, tee_output: bool = True) -> Worker:
    parent_conn, child_conn = _spawn.Pipe()
    child = ChildWorker(predictor_ref, events=child_conn, tee_output=tee_output)
    parent = Worker(child=child, events=parent_conn)
    return parent


def _prepare_payload(payload: Dict[str, Any]) -> None:
    for k, v in payload.items():
        # Check if v is an instance of URLPath
        if isinstance(v, URLPath):
            payload[k] = v.convert()
        # Check if v is a list of URLPath instances
        elif isinstance(v, list) and all(isinstance(item, URLPath) for item in v):
            payload[k] = [item.convert() for item in v]
