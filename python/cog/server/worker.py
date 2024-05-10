import asyncio
import contextlib
import inspect
import multiprocessing
import signal
import sys
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
    PredictionOutput,
    PredictionOutputType,
    PublicEventType,
    Shutdown,
)
from .exceptions import (
    CancelationException,
    FatalWorkerException,
)
from .helpers import StreamRedirector, WrappedStream

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
        self.outs: "defaultdict[str, asyncio.Queue[PublicEventType]]" = defaultdict(
            asyncio.Queue
        )
        self.terminating = terminating
        self.fatal: "Optional[FatalWorkerException]" = None

    async def write(self, id: str, item: PublicEventType) -> None:
        await self.outs[id].put(item)

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

        self._setup()
        self._loop()
        self._stream_redirector.shutdown()
        self._events.close()

    def _setup(self) -> None:
        with self._handle_setup_error():
            # we need to load the predictor to know if setup is async
            self._predictor = load_predictor_from_ref(self._predictor_ref)
            self._predictor.log = self._log
            # if users want to access the same event loop from setup and predict,
            # both have to be async. if setup isn't async, it doesn't matter if we
            # create the event loop here or after setup
            #
            # otherwise, if setup is sync and the user does new_event_loop to use a ClientSession,
            # then tries to use the same session from async predict, they would get an error.
            # that's significant if connections are open and would need to be discarded
            if is_async_predictor(self._predictor):
                self.loop = get_loop()
            # Could be a function or a class
            if hasattr(self._predictor, "setup"):
                if inspect.iscoroutinefunction(self._predictor.setup):
                    self.loop.run_until_complete(run_setup_async(self._predictor))
                else:
                    run_setup(self._predictor)

    @contextlib.contextmanager
    def _handle_setup_error(self) -> Iterator[None]:
        done = Done()
        try:
            yield
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
            self._stream_redirector.drain()
            self._events.send(("SETUP", done))

    def _loop_sync(self) -> None:
        while True:
            ev = self._events.recv()
            if isinstance(ev, Shutdown):
                break
            if isinstance(ev, PredictionInput):
                self._predict_sync(ev)
            elif isinstance(ev, Cancel):
                # in sync mode, Cancel events are ignored
                # only signals are respected
                pass
            else:
                print(f"Got unexpected event: {ev}", file=sys.stderr)

    async def _loop_async(self) -> None:
        events: "AsyncConnection[tuple[str, PublicEventType]]" = AsyncConnection(
            self._events
        )
        with events:
            tasks: "dict[str, asyncio.Task[None]]" = {}
            while True:
                try:
                    ev = await events.recv()
                except asyncio.CancelledError:
                    return
                if isinstance(ev, Shutdown):
                    return
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

    def _loop(self) -> None:
        if is_async(get_predict(self._predictor)):
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
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
        finally:
            self.prediction_id_context.reset(token)
            self._cancelable = False
        self._stream_redirector.drain()
        self._events.send((id, done))

    def _mk_send(self, id: str) -> Callable[[PublicEventType], None]:
        def send(event: PublicEventType) -> None:
            self._events.send((id, event))

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
        if self._predictor and is_async(get_predict(self._predictor)):
            # we could try also canceling the async task around here
            # but for now in async mode signals are ignored
            return
        # this logic might need to be refined
        if signum == signal.SIGUSR1 and self._cancelable:
            raise CancelationException()

    def _log(self, *messages: str, source: str = "stderr") -> None:
        id = self.prediction_id_context.get("LOG")
        self._events.send((id, Log(" ".join(messages), source=source)))

    def _stream_write_hook(
        self, stream_name: str, original_stream: TextIO, data: str
    ) -> None:
        if self._tee_output:
            original_stream.write(data)
            original_stream.flush()
        # this won't work, this fn gets called from a thread, not the async task
        self._log(data, stream_name)


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
