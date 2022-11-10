import multiprocessing
import os
import signal
import sys
import traceback
import types
from enum import Enum, auto, unique

from ..json import make_encodeable
from ..predictor import load_predictor_from_ref
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


@unique
class WorkerState(Enum):
    NEW = auto()
    STARTING = auto()
    READY = auto()
    PROCESSING = auto()
    DEFUNCT = auto()


class Worker:
    def __init__(self, predictor_ref):
        self._state = WorkerState.NEW

        # A pipe with which to communicate with the child worker.
        self._events, child_events = _spawn.Pipe()
        self._child = _ChildWorker(predictor_ref, child_events)
        self._terminating = False

    def setup(self):
        self._assert_state(WorkerState.NEW)
        self._state = WorkerState.STARTING
        self._child.start()

        return self._wait(raise_on_error="Predictor errored during setup")

    def predict(self, payload, poll=None):
        self._assert_state(WorkerState.READY)
        self._state = WorkerState.PROCESSING
        self._events.send(PredictionInput(payload=payload))

        return self._wait(poll=poll)

    def shutdown(self):
        if self._state == WorkerState.DEFUNCT:
            return

        self._terminating = True

        if self._child.is_alive():
            self._events.send(Shutdown())

    def terminate(self):
        if self._state == WorkerState.DEFUNCT:
            return

        self._state = WorkerState.DEFUNCT

        if self._child.is_alive():
            self._child.terminate()
            self._child.join()
        self._child.close()

    def cancel(self):
        if self._state == WorkerState.PROCESSING and self._child.is_alive():
            os.kill(self._child.pid, signal.SIGUSR1)

    def _assert_state(self, state):
        if self._state != state:
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be {state})"
            )

    def _wait(self, poll=None, raise_on_error=None):
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
                raise FatalWorkerException(raise_on_error)
            else:
                self._state = WorkerState.READY

        # If we dropped off the end off the end of the loop, check if it's
        # because the child process died.
        if not self._child.is_alive() and not self._terminating:
            exitcode = self._child.exitcode
            raise FatalWorkerException(
                f"Prediction failed for an unknown reason. It might have run out of memory? (exitcode {exitcode})"
            )


class _ChildWorker(_spawn.Process):
    def __init__(self, predictor_ref, events):
        self._predictor_ref = predictor_ref
        self._predictor = None
        self._events = events
        self._cancelable = False

        super().__init__()

    def run(self):
        # We use SIGUSR1 to signal an interrupt for cancelation.
        signal.signal(signal.SIGUSR1, self._signal_handler)

        ws_stdout = WrappedStream("stdout", sys.stdout)
        ws_stderr = WrappedStream("stderr", sys.stderr)
        self._stream_redirector = StreamRedirector([ws_stdout, ws_stderr], self._stream_write_hook)
        self._stream_redirector.redirect()
        self._stream_redirector.start()

        self._setup()
        self._loop()

        self._stream_redirector.shutdown()

    def _setup(self):
        done = Done()
        try:
            self._predictor = load_predictor_from_ref(self._predictor_ref)
            self._predictor.setup()
        except Exception:
            traceback.print_exc()
            done.error = True
        except:  # for SystemExit and friends reraise to ensure the process dies
            traceback.print_exc()
            done.error = True
            raise
        finally:
            self._stream_redirector.drain()
            self._events.send(done)

    def _loop(self):
        while True:
            done = Done()

            try:
                ev = self._events.recv()
                if isinstance(ev, Shutdown):
                    break
                elif isinstance(ev, PredictionInput):
                    self._predict(done, ev.payload)
                else:
                    print(f"Got unexpected event: {ev}", file=sys.stderr)
            except EOFError:
                done.error = True
                raise RuntimeError("Connection to Worker unexpectedly closed.")
            self._stream_redirector.drain()
            self._events.send(done)

    def _predict(self, done, payload):
        try:
            self._cancelable = True
            result = self._predictor.predict(**payload)
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

    def _signal_handler(self, signum, frame):
        if signum == signal.SIGUSR1 and self._cancelable:
            raise CancelationException()

    def _stream_write_hook(self, stream_name, original_stream, data):
        original_stream.write(data)
        self._events.send(Log(data, source=stream_name))
