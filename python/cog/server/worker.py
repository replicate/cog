import asyncio
import contextlib
import inspect
import logging
import multiprocessing
import os
import signal
import sys
import traceback
import types
from collections import defaultdict
from enum import Enum, auto, unique
from multiprocessing.connection import Connection
from typing import Any, AsyncIterator, Callable, Dict, Iterator, Optional, TextIO, Union

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
    Done,
    Heartbeat,
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
from .helpers import StreamRedirector, WrappedStream

_spawn = multiprocessing.get_context("spawn")

_PublicEventType = Union[Done, Heartbeat, Log, PredictionOutput, PredictionOutputType]


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
        self.outs: "defaultdict[str, asyncio.Queue[_PublicEventType]]" = defaultdict(
            asyncio.Queue
        )
        self.terminating = terminating
        self.fatal: "Optional[FatalWorkerException]" = None

    async def write(self, id: str, item: _PublicEventType) -> None:
        await self.outs[id].put(item)

    async def read(
        self, id: str, poll: Optional[float] = None
    ) -> AsyncIterator[_PublicEventType]:
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


class Worker:
    def __init__(
        self, predictor_ref: str, tee_output: bool = True, concurrent: int = 1
    ) -> None:
        self._state = WorkerState.NEW
        self._allow_cancel = False
        self._semaphore = asyncio.Semaphore(concurrent)
        self._concurrent = concurrent

        # A pipe with which to communicate with the child worker.
        events, child_events = _spawn.Pipe()
        self._child = _ChildWorker(predictor_ref, child_events, tee_output)
        self._events: "AsyncConnection[tuple[str, _PublicEventType]]" = AsyncConnection(
            events
        )
        # shutdown requested
        self._shutting_down = False
        # stop reading events
        self._terminating = asyncio.Event()
        self._mux = Mux(self._terminating)
        self._predictions_in_flight = set()

    def setup(self) -> AsyncIterator[_PublicEventType]:
        self._assert_state(WorkerState.NEW)
        self._state = WorkerState.STARTING

        async def inner() -> AsyncIterator[_PublicEventType]:
            # in 3.10 Event started doing get_running_loop
            # previously it stored the loop when created, which causes an error in tests
            if sys.version_info < (3, 10):
                self._terminating = self._mux.terminating = asyncio.Event()

            self._child.start()
            self._ensure_event_reader()
            async for event in self._mux.read("SETUP", poll=0.1):
                yield event
                if isinstance(event, Done):
                    if event.error:
                        raise FatalWorkerException(
                            "Predictor errored during setup: " + event.error_detail
                        )
                    self._state = WorkerState.IDLE

        return inner()

    def state_from_predictions_in_flight(self) -> WorkerState:
        valid_states = {WorkerState.IDLE, WorkerState.PROCESSING, WorkerState.BUSY}
        if self._state not in valid_states:
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be IDLE, PROCESSING, or BUSY)"
            )
        # changing _allow_cancel like this is a little bit weird
        # because this is kind of a pure function
        # however, we'll remove all of this cancel logic soon, dwabi
        if len(self._predictions_in_flight) == self._concurrent:
            self._allow_cancel = True
            return WorkerState.BUSY
        if len(self._predictions_in_flight) == 0:
            self._allow_cancel = False
            return WorkerState.IDLE
        self._allow_cancel = True
        return WorkerState.PROCESSING

    def is_busy(self) -> bool:
        return self._state not in {WorkerState.PROCESSING, WorkerState.IDLE}

    @contextlib.asynccontextmanager
    async def _prediction_ctx(self, input: PredictionInput) -> AsyncIterator[None]:
        async with self._semaphore:
            self._predictions_in_flight.add(input.id)  # idempotent ig
            self._state = self.state_from_predictions_in_flight()
            try:
                yield
            finally:
                self._predictions_in_flight.remove(input.id)
        self._state = self.state_from_predictions_in_flight()

    def predict(
        self, payload: Dict[str, Any], poll: Optional[float] = None
    ) -> AsyncIterator[_PublicEventType]:
        # this has to be eager for hypothesis
        if self.is_busy():
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be processing or idle)"
            )
        if self._shutting_down:
            raise InvalidStateException(
                "cannot accept new predictions because shutdown requested"
            )
        input = PredictionInput(payload=payload)
        self._predictions_in_flight.add(input.id)  # idempotent ig
        self._state = self.state_from_predictions_in_flight()

        async def inner() -> AsyncIterator[_PublicEventType]:
            async with self._prediction_ctx(input):
                self._events.send(input)
                async for event in self._mux.read(input.id, poll=poll):
                    yield event

        return inner()

    def shutdown(self) -> None:
        if self._state == WorkerState.DEFUNCT:
            return
        # shutdown requested, but keep reading events
        self._shutting_down = True

        if self._child.is_alive():
            self._events.send(Shutdown())

    def terminate(self) -> None:
        if self._state == WorkerState.DEFUNCT:
            return

        self._terminating.set()
        self._state = WorkerState.DEFUNCT

        if self._child.is_alive():
            self._child.terminate()
            self._child.join()
        if self._read_events_task:
            self._read_events_task.cancel()
        self._events.close()

    # FIXME: this will need to use a combination
    # of signals and Cancel events on the pipe
    def cancel(self) -> None:
        if (
            self._allow_cancel
            and self._child.is_alive()
            and self._child.pid is not None
        ):
            os.kill(self._child.pid, signal.SIGUSR1)
            # this should probably check self._semaphore._value == self._concurrent
            self._allow_cancel = False

    def _assert_state(self, state: WorkerState) -> None:
        if self._state != state:
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be {state})"
            )

    _read_events_task: "Optional[asyncio.Task[None]]" = None

    def _ensure_event_reader(self) -> None:
        def handle_error(task: "asyncio.Task[None]") -> None:
            if task.cancelled():
                return
            exc = task.exception()
            if exc:
                logging.error("caught exception", exc_info=exc)

        if not self._read_events_task:
            self._read_events_task = asyncio.create_task(self._read_events())
            self._read_events_task.add_done_callback(handle_error)

    async def _read_events(self) -> None:
        while self._child.is_alive() and not self._terminating.is_set():
            # this can still be running when the task is destroyed
            result = await self._events.recv()  # this might be kind of risky
            # event loop closed or child died
            if result is None:  # type: ignore
                break
            id, event = result
            if id == "LOG" and self._state == WorkerState.STARTING:
                id = "SETUP"
            if id == "LOG" and len(self._predictions_in_flight) == 1:
                id = list(self._predictions_in_flight)[0]
            await self._mux.write(id, event)
        # If we dropped off the end off the end of the loop, check if it's
        # because the child process died.
        if not self._child.is_alive() and not self._terminating.is_set():
            exitcode = self._child.exitcode
            self._mux.fatal = FatalWorkerException(
                f"Prediction failed for an unknown reason. It might have run out of memory? (exitcode {exitcode})"
            )
        # this is the same event as _terminating
        # we need to set it so mux.reads wake up and throw an error if needed
        self._mux.terminating.set()


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
        self._setup()
        self._loop()
        self._stream_redirector.shutdown()
        self._events.close()

    def _setup(self) -> None:
        with self._handle_setup_error():
            # we need to load the predictor to know if setup is async
            self._predictor = load_predictor_from_ref(self._predictor_ref)
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
            else:
                print(f"Got unexpected event: {ev}", file=sys.stderr)

    async def _loop_async(self) -> None:
        events: "AsyncConnection[tuple[str, _PublicEventType]]" = AsyncConnection(
            self._events
        )
        while True:
            try:
                ev = await events.recv()
            except asyncio.CancelledError:
                return
            if isinstance(ev, Shutdown):
                return
            if isinstance(ev, PredictionInput):
                # keep track of these so they can be cancelled
                await self._predict_async(ev)
            # handle Cancel
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
        try:
            yield
        except CancelationException:
            done.canceled = True
        except Exception as e:
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
        finally:
            self._cancelable = False
        self._stream_redirector.drain()
        self._events.send((id, done))

    def _mk_send(self, id: str) -> Callable[[_PublicEventType], None]:
        def send(event: _PublicEventType) -> None:
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
                    result = await result
                    send(PredictionOutputType(multi=False))
                    send(PredictionOutput(payload=make_encodeable(result)))

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
        if signum == signal.SIGUSR1 and self._cancelable:
            raise CancelationException()

    def _stream_write_hook(
        self, stream_name: str, original_stream: TextIO, data: str
    ) -> None:
        if self._tee_output:
            original_stream.write(data)
            original_stream.flush()
        self._events.send(("LOG", Log(data, source=stream_name)))


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
