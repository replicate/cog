import asyncio
import contextlib
import logging
import multiprocessing
import os
import signal
import sys
import threading
import traceback
import typing  # TypeAlias, py3.10
from datetime import datetime, timezone
from enum import Enum, auto, unique
from typing import Any, AsyncIterator, Awaitable, Iterator, Optional, Union

import httpx
import structlog
from attrs import define
from fastapi.encoders import jsonable_encoder

from .. import schema, types
from .clients import SKIP_START_EVENT, ClientManager
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
    FatalWorkerException,
    InvalidStateException,
)
from .helpers import AsyncPipe
from .probes import ProbeHelper
from .worker import Mux, _ChildWorker

log = structlog.get_logger("cog.server.runner")
_spawn = multiprocessing.get_context("spawn")


class FileUploadError(Exception):
    pass


class RunnerBusyError(Exception):
    pass


class UnknownPredictionError(Exception):
    pass


@unique
class WorkerState(Enum):
    NEW = auto()
    STARTING = auto()
    IDLE = auto()
    PROCESSING = auto()
    BUSY = auto()
    DEFUNCT = auto()


@define
class SetupResult:
    started_at: datetime
    completed_at: datetime
    logs: str
    status: schema.Status

    # TODO: maybe collect events into a result here


PredictionTask: "typing.TypeAlias" = "asyncio.Task[schema.PredictionResponse]"
SetupTask: "typing.TypeAlias" = "asyncio.Task[SetupResult]"
RunnerTask: "typing.TypeAlias" = Union[PredictionTask, SetupTask]


class PredictionRunner:
    def __init__(
        self,
        *,
        predictor_ref: str,
        shutdown_event: Optional[threading.Event],
        upload_url: Optional[str] = None,
        concurrency: int = 1,
        tee_output: bool = True,
    ) -> None:
        self._shutdown_event = shutdown_event  # __main__ waits for this event
        self._upload_url = upload_url
        self._predictions: "dict[str, tuple[schema.PredictionResponse, PredictionTask]]" = (
            {}
        )
        self._predictions_in_flight: "set[str]" = set()
        # it would be lovely to merge these but it's not fully clear how best to handle it
        # since idempotent requests can kinda come whenever?
        # p: dict[str, PredictionTask]
        # p: dict[str, PredictionEventHandler]
        # p: dict[str, schema.PredictionResponse]

        self.client_manager = ClientManager()

        # worker code
        self._state = WorkerState.NEW
        self._semaphore = asyncio.Semaphore(concurrency)
        self._concurrency = concurrency

        # A pipe with which to communicate with the child worker.
        events, child_events = _spawn.Pipe()
        self._child = _ChildWorker(predictor_ref, child_events, tee_output)
        self._events: "AsyncPipe[tuple[str, PublicEventType]]" = AsyncPipe(
            events, self._child.is_alive
        )
        # shutdown requested
        self._shutting_down = False
        # stop reading events
        self._terminating = asyncio.Event()
        self._mux = Mux(self._terminating)

    def setup(self) -> SetupTask:
        if self._state != WorkerState.NEW:
            raise RunnerBusyError
        self._state = WorkerState.STARTING

        # app is allowed to respond to requests and poll the state of this task
        # while it is running
        async def inner() -> SetupResult:
            logs = []
            status = None
            started_at = datetime.now(tz=timezone.utc)

            # in 3.10 Event started doing get_running_loop
            # previously it stored the loop when created, which causes an error in tests
            if sys.version_info < (3, 10):
                self._terminating = self._mux.terminating = asyncio.Event()

            self._child.start()
            self._ensure_event_reader()

            try:
                async for event in self._mux.read("SETUP", poll=0.1):
                    if isinstance(event, Log):
                        logs.append(event.message)
                    elif isinstance(event, Done):
                        if event.error:
                            raise FatalWorkerException(
                                "Predictor errored during setup: " + event.error_detail
                            )
                            status = schema.Status.FAILED
                        else:
                            status = schema.Status.SUCCEEDED
                        self._state = WorkerState.IDLE
            except Exception:
                logs.append(traceback.format_exc())
                status = schema.Status.FAILED

            if status is None:
                logs.append("Error: did not receive 'done' event from setup!")
                status = schema.Status.FAILED

            completed_at = datetime.now(tz=timezone.utc)

            # Only if setup succeeded, mark the container as "ready".
            if status == schema.Status.SUCCEEDED:
                probes = ProbeHelper()
                probes.ready()

            return SetupResult(
                started_at=started_at,
                completed_at=completed_at,
                logs="".join(logs),
                status=status,
            )

        def handle_error(task: RunnerTask) -> None:
            exc = task.exception()
            if not exc:
                return
            # Re-raise the exception in order to more easily capture exc_info,
            # and then trigger shutdown, as we have no easy way to resume
            # worker state if an exception was thrown.
            try:
                raise exc
            except Exception:
                log.error("caught exception while running setup", exc_info=True)
                if self._shutdown_event is not None:
                    self._shutdown_event.set()

        result = asyncio.create_task(inner())
        result.add_done_callback(handle_error)
        return result

    def state_from_predictions_in_flight(self) -> WorkerState:
        valid_states = {WorkerState.IDLE, WorkerState.PROCESSING, WorkerState.BUSY}
        if self._state not in valid_states:
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be IDLE, PROCESSING, or BUSY)"
            )
        if len(self._predictions_in_flight) == self._concurrency:
            return WorkerState.BUSY
        if len(self._predictions_in_flight) == 0:
            return WorkerState.IDLE
        return WorkerState.PROCESSING

    def is_busy(self) -> bool:
        return self._state not in {WorkerState.PROCESSING, WorkerState.IDLE}

    def enter_predict(self, id: str) -> None:
        if self.is_busy():
            raise InvalidStateException(
                f"Invalid operation: state is {self._state} (must be processing or idle)"
            )
        if self._shutting_down:
            raise InvalidStateException(
                "cannot accept new predictions because shutdown requested"
            )
        log.info("accepted prediction %s in flight %s", id, self._predictions_in_flight)
        self._predictions_in_flight.add(id)
        self._state = self.state_from_predictions_in_flight()

    def exit_predict(self, id: str) -> None:
        self._predictions_in_flight.remove(id)
        self._state = self.state_from_predictions_in_flight()

    @contextlib.contextmanager
    def prediction_ctx(self, id: str) -> Iterator[None]:
        self.enter_predict(id)
        try:
            yield
        finally:
            self.exit_predict(id)

    # TODO: Make the return type AsyncResult[schema.PredictionResponse] when we
    # no longer have to support Python 3.8
    def predict(
        self, request: schema.PredictionRequest, poll: Optional[float] = None
    ) -> "tuple[schema.PredictionResponse, PredictionTask]":
        if self.is_busy():
            if request.id in self._predictions:
                return self._predictions[request.id]
            raise RunnerBusyError()

        # Set up logger context for main thread.
        structlog.contextvars.clear_contextvars()
        structlog.contextvars.bind_contextvars(prediction_id=request.id)

        # if upload url was not set, we can respect output_file_prefix
        # but maybe we should just throw an error
        upload_url = request.output_file_prefix or self._upload_url
        # this is supposed to send START, but we're trapped in a sync function
        event_handler = PredictionEventHandler(request, self.client_manager, upload_url)
        response = event_handler.response

        prediction_input = PredictionInput.from_request(request)
        self.enter_predict(request.id)

        async def async_predict_handling_errors() -> schema.PredictionResponse:
            try:
                # FIXME: handle e.g. dict[str, list[Path]]
                # FIXME: download files concurrently
                for k, v in prediction_input.payload.items():
                    if isinstance(v, types.DataURLTempFilePath):
                        prediction_input.payload[k] = v.convert()
                    if isinstance(v, types.URLTempFile):
                        real_path = await v.convert(self.client_manager.download_client)
                        prediction_input.payload[k] = real_path
                async with self._semaphore:
                    self._events.send(prediction_input)
                    event_stream = self._mux.read(prediction_input.id, poll=poll)
                    result = await event_handler.handle_event_stream(event_stream)
                    return result
            except httpx.HTTPError as e:
                tb = traceback.format_exc()
                await event_handler.append_logs(tb)
                await event_handler.failed(error=str(e))
                log.warn("failed to download url path from input", exc_info=True)
                return event_handler.response
            except Exception as e:
                tb = traceback.format_exc()
                await event_handler.append_logs(tb)
                await event_handler.failed(error=str(e))
                log.error("caught exception while running prediction", exc_info=True)
                if self._shutdown_event is not None:
                    self._shutdown_event.set()
                raise  # we don't actually want to raise anymore but w/e
            finally:
                # mark the prediction as done and update state
                # ... actually, we might want to mark that part earlier
                # even if we're still uploading files we can accept new work
                self.exit_predict(prediction_input.id)
                # FIXME: use isinstance(BaseInput)
                if hasattr(request.input, "cleanup"):
                    request.input.cleanup()  # type: ignore
                # this might also, potentially, be too early
                # since this is just before this coroutine exits
                self._predictions.pop(request.id)

        # this is still a little silly
        result = asyncio.create_task(async_predict_handling_errors())
        # result.add_done_callback(self.make_error_handler("prediction"))
        # even after inlining we might still need a callback to surface remaining exceptions/results
        self._predictions[request.id] = (response, result)

        return (response, result)

    def shutdown(self) -> None:
        if self._state == WorkerState.DEFUNCT:
            return
        # shutdown requested, but keep reading events
        self._shutting_down = True

        if self._child.is_alive():
            self._events.send(Shutdown())

    def terminate(self) -> None:
        for _, task in self._predictions.values():
            task.cancel()
        if self._state == WorkerState.DEFUNCT:
            return

        self._terminating.set()
        self._state = WorkerState.DEFUNCT

        if self._child.is_alive():
            self._child.terminate()
            self._child.join()
        self._events.shutdown()
        if self._read_events_task:
            self._read_events_task.cancel()

    def cancel(self, prediction_id: str) -> None:
        if id not in self._predictions_in_flight:
            log.warn("can't cancel %s (%s)", prediction_id, self._predictions_in_flight)
            raise UnknownPredictionError()
        if self._child.is_alive() and self._child.pid is not None:
            os.kill(self._child.pid, signal.SIGUSR1)
            log.info("sent cancel")
            self._events.send(Cancel(prediction_id))
            # maybe this should probably check self._semaphore._value == self._concurrent

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
            # in tests this can still be running when the task is destroyed
            result = await self._events.coro_recv_with_exit(self._terminating)
            if result is None:  # event loop closed or child died
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
        # this is the same event as self._terminating
        # we need to set it so mux.reads wake up and throw an error if needed
        self._mux.terminating.set()


class PredictionEventHandler:
    def __init__(
        self,
        request: schema.PredictionRequest,
        client_manager: ClientManager,
        upload_url: Optional[str],
    ) -> None:
        log.info("starting prediction")
        self.p = schema.PredictionResponse(**request.dict())
        self.p.status = schema.Status.PROCESSING
        self.p.output = None
        self.p.logs = ""
        self.p.started_at = datetime.now(tz=timezone.utc)

        self._client_manager = client_manager
        self._webhook_sender = client_manager.make_webhook_sender(
            request.webhook, request.webhook_events_filter
        )
        self._upload_url = upload_url
        self._output_type = None

        # HACK: don't send an initial webhook if we're trying to optimize for
        # latency (this guarantees that the first output webhook won't be
        # throttled.)
        if not SKIP_START_EVENT:
            # idk
            # this is pretty wrong
            asyncio.create_task(self._send_webhook(schema.WebhookEvent.START))

    @property
    def response(self) -> schema.PredictionResponse:
        return self.p

    async def set_output(self, output: Any) -> None:
        assert self.p.output is None, "Predictor unexpectedly returned multiple outputs"
        self.p.output = await self._upload_files(output)
        # We don't send a webhook for compatibility with the behaviour of
        # redis_queue. In future we can consider whether it makes sense to send
        # one here.

    async def append_output(self, output: Any) -> None:
        assert isinstance(
            self.p.output, list
        ), "Cannot append output before setting output"
        self.p.output.append(await self._upload_files(output))
        await self._send_webhook(schema.WebhookEvent.OUTPUT)

    async def append_logs(self, logs: str) -> None:
        assert self.p.logs is not None
        self.p.logs += logs
        await self._send_webhook(schema.WebhookEvent.LOGS)

    async def succeeded(self) -> None:
        log.info("prediction succeeded")
        self.p.status = schema.Status.SUCCEEDED
        self._set_completed_at()
        # These have been set already: this is to convince the typechecker of
        # that...
        assert self.p.completed_at is not None
        assert self.p.started_at is not None
        self.p.metrics = {
            "predict_time": (self.p.completed_at - self.p.started_at).total_seconds()
        }
        await self._send_webhook(schema.WebhookEvent.COMPLETED)

    async def failed(self, error: str) -> None:
        log.info("prediction failed", error=error)
        self.p.status = schema.Status.FAILED
        self.p.error = error
        self._set_completed_at()
        await self._send_webhook(schema.WebhookEvent.COMPLETED)

    async def canceled(self) -> None:
        log.info("prediction canceled")
        self.p.status = schema.Status.CANCELED
        self._set_completed_at()
        await self._send_webhook(schema.WebhookEvent.COMPLETED)

    def _set_completed_at(self) -> None:
        self.p.completed_at = datetime.now(tz=timezone.utc)

    async def _send_webhook(self, event: schema.WebhookEvent) -> None:
        dict_response = jsonable_encoder(self.response.dict(exclude_unset=True))
        await self._webhook_sender(dict_response, event)

    async def _upload_files(self, output: Any) -> Any:
        try:
            # TODO: clean up output files
            return await self._client_manager.upload_files(output, self._upload_url)
        except Exception as error:
            # If something goes wrong uploading a file, it's irrecoverable.
            # The re-raised exception will be caught and cause the prediction
            # to be failed, with a useful error message.
            raise FileUploadError("Got error trying to upload output files") from error

    async def handle_event_stream(
        self, events: AsyncIterator[PublicEventType]
    ) -> schema.PredictionResponse:
        async for event in events:
            await self.event_to_handle_future(event)
            if self.p.status == schema.Status.FAILED:
                break
        return self.response

    async def noop(self) -> None:
        pass

    def event_to_handle_future(self, event: PublicEventType) -> Awaitable[None]:
        if isinstance(event, Heartbeat):
            # Heartbeat events exist solely to ensure that we have a
            # regular opportunity to check for cancelation and
            # timeouts.
            # We don't need to do anything with them.
            return self.noop()
        if isinstance(event, Log):
            return self.append_logs(event.message)

        if isinstance(event, PredictionOutputType):
            if self._output_type is not None:
                return self.failed(error="Predictor returned unexpected output")
            self._output_type = event
            if self._output_type.multi:
                return self.set_output([])
        if isinstance(event, PredictionOutput):
            if self._output_type is None:
                return self.failed(error="Predictor returned unexpected output")
            if self._output_type.multi:
                return self.append_output(event.payload)
            return self.set_output(event.payload)
        if isinstance(event, Done):  # pyright: ignore reportUnnecessaryIsinstance
            if event.canceled:
                return self.canceled()
            if event.error:
                return self.failed(error=str(event.error_detail))
            return self.succeeded()
        log.warn("received unexpected event from worker", data=event)
