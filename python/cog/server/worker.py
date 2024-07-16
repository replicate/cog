import asyncio
import contextlib
import inspect
import multiprocessing
import signal
import sys
import threading
import traceback
import types
from collections import defaultdict
from contextvars import ContextVar
from enum import Enum, auto, unique
from multiprocessing.connection import Connection
from typing import Any, AsyncIterator, Callable, Iterator, Optional, TextIO

from ..json import make_encodeable
from ..predictor import (
    BasePredictor,
    get_predict,
    load_predictor_from_ref,
    run_setup,
    run_setup_async,
)
from .connection import AsyncConnection
from .eventtypes import (
    Cancel,
    Done,
    Heartbeat,
    Log,
    PredictionInput,
    PredictionMetric,
    PredictionOutput,
    PredictionOutputType,
    PublicEventType,
    Shutdown,
)
from .exceptions import (
    CancelationException,
    FatalWorkerException,
)
from .helpers import StreamRedirector, WrappedStream, debug

_spawn = multiprocessing.get_context("spawn")


@unique
class WorkerState(Enum):
    NEW = auto()
    STARTING = auto()
    IDLE = auto()
    PROCESSING = auto()
    BUSY = auto()
    DEFUNCT = auto()


class Mux:
    def __init__(self, terminating: asyncio.Event) -> None:
        self.outs: defaultdict[str, asyncio.Queue[PublicEventType]] = defaultdict(
            asyncio.Queue
        )
        self.terminating = terminating
        self.fatal: Optional[FatalWorkerException] = None

    def write(self, id: str, item: PublicEventType) -> None:
        self.outs[id].put_nowait(item)

    async def read(
        self, id: str, poll: Optional[float] = None
    ) -> AsyncIterator[PublicEventType]:
        if poll:
            send_heartbeats = True
        else:
            poll = 0.1
            send_heartbeats = False
        while not self.terminating.is_set():
            try:
                event = await asyncio.wait_for(self.outs[id].get(), timeout=poll)
            except asyncio.TimeoutError:
                if send_heartbeats:
                    yield Heartbeat()
                continue
            yield event
            if isinstance(event, Done):
                self.outs.pop(id)
                break
        if self.fatal:
            raise self.fatal


# janky mutable container for a single eventual ChildWorker
worker_reference: "dict[None, _ChildWorker]" = {}


def emit_metric(metric_name: str, metric_value: "float | int") -> None:
    worker = worker_reference.get(None, None)
    if worker is None:
        raise Exception("Attempted to emit metric but worker is not running")
    worker._emit_metric(metric_name, metric_value)


class _ChildWorker(_spawn.Process):  # type: ignore

    def __init__(
        self,
        predictor_ref: str,
        events: Connection,
        tee_output: bool = True,
    ) -> None:
        self._predictor_ref = predictor_ref
        self._predictor: Optional[BasePredictor] = None
        self._events = events
        self._events_async: Optional[AsyncConnection[tuple[str, PublicEventType]]] = (
            None
        )
        self._process_logs_task: Optional[asyncio.Task[None]] = None
        self._tee_output = tee_output
        self._cancelable = False

        super().__init__()

    def run(self) -> None:
        debug("run")
        self._sync_events_lock = threading.Lock()
        # If we're running at a shell, SIGINT will be sent to every process in
        # the process group. We ignore it in the child process and require that
        # shutdown is coordinated by the parent process.
        signal.signal(signal.SIGINT, signal.SIG_IGN)

        # We use SIGUSR1 to signal an interrupt for cancelation.
        signal.signal(signal.SIGUSR1, self._signal_handler)

        worker_reference[None] = self
        self.prediction_id_context: ContextVar[str] = ContextVar("prediction_context")

        # <could be moved into StreamRedirector>
        ws_stdout = WrappedStream("stdout", sys.stdout)
        ws_stderr = WrappedStream("stderr", sys.stderr)
        ws_stdout.wrap()
        ws_stderr.wrap()

        # using a thread for this can potentially cause a deadlock
        # however, if we made this async, we might interfere with a user's event loop
        self._stream_redirector = StreamRedirector(
            [ws_stdout, ws_stderr], self._stream_write_hook
        )
        self._stream_redirector.start()
        # </could be moved into StreamRedirector>

        debug("setup")
        self._setup()
        debug("loop")
        self._loop()  # shuts down stream redirector the correct way
        debug("loop done")
        self._events.close()

    async def _async_init(self) -> None:
        debug("async_init start")
        if self._events_async:
            debug("async_init finished")
            return
        self._events_async = AsyncConnection(self._events)
        await self._events_async.async_init()
        await self._stream_redirector.switch_to_async()
        debug("async_init done")

    def _setup(self) -> None:
        debug("_setup start")
        with self._handle_setup_error():
            # we need to load the predictor to know if setup is async
            debug("'about to load")
            self._predictor = load_predictor_from_ref(self._predictor_ref)
            debug("loaded ref")
            self._predictor.log = self._log
            # if users want to access the same event loop from setup and predict,
            # both have to be async. if setup isn't async, it doesn't matter if we
            # create the event loop here or after setup
            #
            # otherwise, if setup is sync and the user does new_event_loop to use a ClientSession,
            # then tries to use the same session from async predict, they would get an error.
            # that's significant if connections are open and would need to be discarded
            debug("async predictor")
            if is_async_predictor(self._predictor):
                debug("getting loop")
                self.loop = get_loop()
                debug("got loop")
            debug("getattr")
            # Could be a function or a class
            if hasattr(self._predictor, "setup"):
                debug("inspect")
                if inspect.iscoroutinefunction(self._predictor.setup):
                    # we should probably handle Shutdown during this process?
                    # debug("creating AsyncConn")
                    self.loop.run_until_complete(self._async_init())
                    self.loop.run_until_complete(run_setup_async(self._predictor))
                else:
                    debug("sync setup")
                    run_setup(self._predictor)
        debug("_setup done")

    @contextlib.contextmanager
    def _handle_setup_error(self) -> Iterator[None]:
        done = Done()
        debug("done")
        try:
            debug("yield")
            yield
            debug("yield done")
        except Exception as e:
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
            debug("setup done, calling drain")
            self._stream_redirector.drain()
            debug("sending setup done")
            self.send(("SETUP", done))
            debug("sent setup done")

    def _loop_sync(self) -> None:
        while True:
            ev = self._events.recv()
            if isinstance(ev, Shutdown):
                self._log("got Shutdown event")
                break
            if isinstance(ev, PredictionInput):
                self._predict_sync(ev)
            elif isinstance(ev, Cancel):
                # in sync mode, Cancel events are ignored
                # only signals are respected
                pass
            else:
                print(f"Got unexpected event: {ev}", file=sys.stderr)
        self._stream_redirector.shutdown()

    async def _loop_async(self) -> None:
        await self._async_init()
        assert self._events_async
        tasks: dict[str, asyncio.Task[None]] = {}
        while True:
            try:
                ev = await self._events_async.recv()
            except asyncio.CancelledError:
                break
            if isinstance(ev, Shutdown):
                self._log("got shutdown event [async]")
                break # should this be return?
            if isinstance(ev, PredictionInput):
                # keep track of these so they can be cancelled
                tasks[ev.id] = asyncio.create_task(self._predict_async(ev))
            elif isinstance(ev, Cancel):
                # in async mode, cancel signals are ignored
                # only Cancel events are ignored
                if ev.id in tasks:
                    tasks[ev.id].cancel()
                else:
                    print(f"Got unexpected cancellation: {ev}", file=sys.stderr)
            else:
                print(f"Got unexpected event: {ev}", file=sys.stderr)
        debug("shutdown_async")
        await self._stream_redirector.shutdown_async()
        self._events_async.close()

    def _loop(self) -> None:
        debug("in loop")
        if is_async(get_predict(self._predictor)):
            debug("async loop")
            self.loop.run_until_complete(self._loop_async())
        else:
            self._loop_sync()

    @contextlib.contextmanager
    def _handle_predict_error(self, id: str) -> Iterator[None]:
        assert self._predictor
        done = Done()
        self._cancelable = True
        token = self.prediction_id_context.set(id)
        try:
            yield
        except CancelationException:
            done.canceled = True
        except asyncio.CancelledError:
            done.canceled = True
        except Exception as e:
            tb = traceback.format_exc()
            self._log(tb)
            done.error = True
            done.error_detail = str(e) if str(e) else repr(e)
        finally:
            self.prediction_id_context.reset(token)
            self._cancelable = False
        self._stream_redirector.drain()
        self.send((id, done))

    def _emit_metric(self, name: str, value: "int | float") -> None:
        prediction_id = self.prediction_id_context.get(None)
        if prediction_id is None:
            raise Exception("Tried to emit a metric outside a prediction context")
        self.send((prediction_id, PredictionMetric(name, value)))

    def send(self, obj: Any) -> None:
        if self._events_async:
            self._events_async.send(obj)
        else:
            with self._sync_events_lock:
                self._events.send(obj)

    def _mk_send(self, id: str) -> Callable[[PublicEventType], None]:
        def send(event: PublicEventType) -> None:
            self.send((id, event))

        return send

    async def _predict_async(self, input: PredictionInput) -> None:
        with self._handle_predict_error(input.id):
            predict = get_predict(self._predictor)
            result = predict(**input.payload)
            send = self._mk_send(input.id)
            if result:
                if inspect.isasyncgen(result):
                    send(PredictionOutputType(multi=True))
                    async for r in result:
                        send(PredictionOutput(payload=make_encodeable(r)))
                elif inspect.isawaitable(result):
                    output = await result
                    send(PredictionOutputType(multi=False))
                    send(PredictionOutput(payload=make_encodeable(output)))

    def _predict_sync(self, input: PredictionInput) -> None:
        with self._handle_predict_error(input.id):
            predict = get_predict(self._predictor)
            result = predict(**input.payload)
            send = self._mk_send(input.id)
            if result:
                if inspect.isgenerator(result):
                    send(PredictionOutputType(multi=True))
                    for r in result:
                        send(PredictionOutput(payload=make_encodeable(r)))
                else:
                    send(PredictionOutputType(multi=False))
                    send(PredictionOutput(payload=make_encodeable(result)))

    def _signal_handler(self, signum: int, frame: Optional[types.FrameType]) -> None:
        # perhaps we should handle shutdown during setup using a signal?
        if self._predictor and is_async(get_predict(self._predictor)):
            # we could try also canceling the async task around here
            # but for now in async mode signals are ignored
            return
        # this logic might need to be refined
        if signum == signal.SIGUSR1 and self._cancelable:
            raise CancelationException()

    def _log(self, *messages: str, source: str = "stderr") -> None:
        id = self.prediction_id_context.get("LOG")
        self.send((id, Log(" ".join(messages), source=source)))

    def _stream_write_hook(
        self, stream_name: str, original_stream: TextIO, data: str
    ) -> None:
        if self._tee_output:
            original_stream.write(data)
            original_stream.flush()
        # this won't record prediction_id, because
        # this fn gets called from a thread, not the async task
        self._log(data, source=stream_name)


def get_loop() -> asyncio.AbstractEventLoop:
    try:
        # just in case something else created an event loop already
        return asyncio.get_running_loop()
    except RuntimeError:
        return asyncio.new_event_loop()


def is_async(fn: Any) -> bool:
    return inspect.iscoroutinefunction(fn) or inspect.isasyncgenfunction(fn)


def is_async_predictor(predictor: BasePredictor) -> bool:
    setup = getattr(predictor, "setup", None)
    return is_async(setup) or is_async(get_predict(predictor))
