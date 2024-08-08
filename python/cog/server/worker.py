import multiprocessing
import os
import signal
import sys
import traceback
import types
from concurrent.futures import Future, ThreadPoolExecutor
from enum import Enum, auto, unique
from multiprocessing.connection import Connection
from typing import Any, Callable, Dict, Optional, TextIO, Union

import structlog

from ..json import make_encodeable
from ..predictor import BasePredictor, get_predict, load_predictor_from_ref, run_setup
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
from .helpers import StreamRedirector, WrappedStream

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
    def __init__(self, predictor_ref: str, tee_output: bool = True) -> None:
        self._state = WorkerState.NEW
        self._allow_cancel = False

        self._result: Optional["Future[Done]"] = None
        self._subscribers: Dict[int, Optional[Callable[[_PublicEventType], None]]] = {}

        self._pool = ThreadPoolExecutor(max_workers=1)
        self._event_consumer = None

        # A pipe with which to communicate with the child worker.
        self._events, child_events = _spawn.Pipe()
        self._child = _ChildWorker(predictor_ref, child_events, tee_output)
        self._sent_shutdown_event = False
        self._terminating = False

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
        self._events.send(PredictionInput(payload=payload))
        return result

    def subscribe(self, subscriber: Callable[[_PublicEventType], None]) -> int:
        idx = len(self._subscribers)
        self._subscribers[idx] = subscriber
        return idx

    def unsubscribe(self, idx: int) -> None:
        self._subscribers[idx] = None

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
        self._result.set_result(done)
        self._result = None
        self._state = WorkerState.READY

        # Predictions
        while True:
            done = self._consume_events_until_done()
            if not done:
                break
            assert self._result
            self._result.set_result(done)
            self._result = None
            self._state = WorkerState.READY
            self._allow_cancel = False

        # If we dropped off the end off the end of the loop, it's because the
        # child process died.
        if not self._terminating:
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
            if subscriber:
                try:
                    subscriber(ev)
                except Exception:
                    log.warn(
                        "publish failed", subscriber=subscriber, ev=ev, exc_info=True
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
                self._stream_redirector.drain(timeout=10)
            except TimeoutError:
                self._events.send(
                    Log(
                        "WARNING: logs may be truncated due to excessive volume.",
                        source="stderr",
                    )
                )
                raise
            self._events.send(done)

    def _loop(self) -> None:
        while True:
            ev = self._events.recv()
            if isinstance(ev, Shutdown):
                break
            if isinstance(ev, PredictionInput):
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
        except Exception as e:  # pylint: disable=broad-exception-caught
            traceback.print_exc()
            done.error = True
            done.error_detail = str(e)
        finally:
            self._cancelable = False
            try:
                self._stream_redirector.drain(timeout=10)
            except TimeoutError:
                self._events.send(
                    Log(
                        "WARNING: logs may be truncated due to excessive volume.",
                        source="stderr",
                    )
                )
                raise
            self._events.send(done)

    def _signal_handler(
        self,
        signum: int,
        frame: Optional[types.FrameType],  # pylint: disable=unused-argument
    ) -> None:
        if signum == signal.SIGUSR1 and self._cancelable:
            raise CancelationException()

    def _stream_write_hook(
        self, stream_name: str, original_stream: TextIO, data: str
    ) -> None:
        if self._tee_output:
            original_stream.write(data)
            original_stream.flush()
        self._events.send(Log(data, source=stream_name))
