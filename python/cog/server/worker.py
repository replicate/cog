import multiprocessing
import os
import signal
import sys
import traceback
import types
from enum import Enum, auto, unique
from multiprocessing.connection import Connection
from typing import Any, Dict, Iterable, Optional, TextIO, Union

from ..json import make_encodeable
from ..predictor import BasePredictor, get_predict, load_predictor_from_ref, run_setup
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
    READY = auto()
    PROCESSING = auto()
    DEFUNCT = auto()


class Worker:
    def __init__(self, predictor_ref: str, tee_output: bool = True) -> None:
        self._state = WorkerState.NEW
        self._allow_cancel = False

        # A pipe with which to communicate with the child worker.
        self._events, child_events = _spawn.Pipe()
        self._child = _ChildWorker(predictor_ref, child_events, tee_output)
        self._terminating = False

    def setup(self) -> Iterable[_PublicEventType]:
        self._assert_state(WorkerState.NEW)
        self._state = WorkerState.STARTING
        self._child.start()

        return self._wait(raise_on_error="Predictor errored during setup")

    def predict(
        self, payload: Dict[str, Any], poll: Optional[float] = None
    ) -> Iterable[_PublicEventType]:
        self._assert_state(WorkerState.READY)
        self._state = WorkerState.PROCESSING
        self._allow_cancel = True
        self._events.send(PredictionInput(payload=payload))

        return self._wait(poll=poll)

    def shutdown(self) -> None:
        if self._state == WorkerState.DEFUNCT:
            return

        self._terminating = True

        if self._child.is_alive():
            self._events.send(Shutdown())

    def terminate(self) -> None:
        if self._state == WorkerState.DEFUNCT:
            return

        self._terminating = True
        self._state = WorkerState.DEFUNCT

        if self._child.is_alive():
            self._child.terminate()
            self._child.join()

    def cancel(self) -> None:
        if (
            self._allow_cancel
            and self._child.is_alive()
            and self._child.pid is not None
        ):
            os.kill(self._child.pid, signal.SIGUSR1)
            self._allow_cancel = False

    def _assert_state(self, state: WorkerState) -> None:
        if self._state != state:
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be {state})"
            )

    def _wait(
        self, poll: Optional[float] = None, raise_on_error: Optional[str] = None
    ) -> Iterable[_PublicEventType]:
        done = None

        if poll:
            send_heartbeats = True
        else:
            poll = 0.1
            send_heartbeats = False

        while self._child.is_alive() and not done:
            if not self._events.poll(poll):
                if send_heartbeats:
                    yield Heartbeat()
                continue

            ev = self._events.recv()
            yield ev

            if isinstance(ev, Done):
                done = ev

        if done:
            if done.error and raise_on_error:
                raise FatalWorkerException(raise_on_error + ": " + done.error_detail)
            else:
                self._state = WorkerState.READY
                self._allow_cancel = False

        # If we dropped off the end off the end of the loop, check if it's
        # because the child process died.
        if not self._child.is_alive() and not self._terminating:
            exitcode = self._child.exitcode
            raise FatalWorkerException(
                f"Prediction failed for an unknown reason. It might have run out of memory? (exitcode {exitcode})"
            )


class LockedConn:
    def __init__(self, conn: Connection) -> None:
        self.conn = conn
        self._lock = _spawn.Lock()

    def send(self, obj: Any) -> None:
        with self._lock:
            self.conn.send(obj)

    def recv(self) -> Any:
        return self.conn.recv()


class _ChildWorker(_spawn.Process):  # type: ignore
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

        ws_stdout = WrappedStream("stdout", sys.stdout)
        ws_stderr = WrappedStream("stderr", sys.stderr)
        ws_stdout.wrap()
        ws_stderr.wrap()

        self._stream_redirector = StreamRedirector(
            [ws_stdout, ws_stderr], self._stream_write_hook
        )
        self._stream_redirector.start()

        self._setup()
        self._loop()

        self._stream_redirector.shutdown()

    def _setup(self) -> None:
        done = Done()
        try:
            self._predictor = load_predictor_from_ref(self._predictor_ref)
            # Could be a function or a class
            if hasattr(self._predictor, "setup"):
                run_setup(self._predictor)
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
            self._events.send(done)

    def _loop(self) -> None:
        while True:
            ev = self._events.recv()
            if isinstance(ev, Shutdown):
                break
            elif isinstance(ev, PredictionInput):
                self._predict(ev.payload)
            else:
                print(f"Got unexpected event: {ev}", file=sys.stderr)

    def _predict(self, payload: Dict[str, Any]) -> None:
        assert self._predictor
        done = Done()
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
        except Exception as e:
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
        finally:
            self._cancelable = False
        self._stream_redirector.drain()
        self._events.send(done)

    def _signal_handler(self, signum: int, frame: Optional[types.FrameType]) -> None:
        if signum == signal.SIGUSR1 and self._cancelable:
            raise CancelationException()

    def _stream_write_hook(
        self, stream_name: str, original_stream: TextIO, data: str
    ) -> None:
        if self._tee_output:
            original_stream.write(data)
            original_stream.flush()
        self._events.send(Log(data, source=stream_name))
