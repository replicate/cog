import asyncio
import multiprocessing
import threading
import traceback
import typing  # TypeAlias, py3.10
from datetime import datetime, timezone
from typing import Any, AsyncIterator, Awaitable, Callable, Optional, Tuple, Union, cast

import httpx
import structlog
from attrs import define
from fastapi.encoders import jsonable_encoder

from .. import schema, types
from .clients import SKIP_START_EVENT, ClientManager
from .eventtypes import (
    Done,
    Heartbeat,
    Log,
    PredictionInput,
    PredictionOutput,
    PredictionOutputType,
    PublicEventType,
)
from .probes import ProbeHelper
from .worker import Worker

log = structlog.get_logger("cog.server.runner")
_spawn = multiprocessing.get_context("spawn")


class FileUploadError(Exception):
    pass


class RunnerBusyError(Exception):
    pass


class UnknownPredictionError(Exception):
    pass


@define
class SetupResult:
    started_at: datetime
    completed_at: datetime
    logs: str
    status: schema.Status


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
    ) -> None:
        self._response: Optional[schema.PredictionResponse] = None
        self._result: Optional[RunnerTask] = None

        self._worker = Worker(predictor_ref=predictor_ref)

        self._shutdown_event = shutdown_event
        self._upload_url = upload_url

        self.client_manager = ClientManager()

    def make_error_handler(self, activity: str) -> Callable[[RunnerTask], None]:
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
                log.error(f"caught exception while running {activity}", exc_info=True)
                if self._shutdown_event is not None:
                    self._shutdown_event.set()

        return handle_error

    def setup(self) -> SetupTask:
        if self.is_busy():
            raise RunnerBusyError()
        self._result = asyncio.create_task(setup(worker=self._worker))
        self._result.add_done_callback(self.make_error_handler("setup"))
        return self._result

    # TODO: Make the return type AsyncResult[schema.PredictionResponse] when we
    # no longer have to support Python 3.8
    def predict(
        self, request: schema.PredictionRequest, upload: bool = True
    ) -> Tuple[schema.PredictionResponse, PredictionTask]:
        # It's the caller's responsibility to not call us if we're busy.
        if self.is_busy():
            # If self._result is set, but self._response is not, we're still
            # doing setup.
            if self._response is None:
                raise RunnerBusyError()
            assert self._result is not None
            if request.id is not None and request.id == self._response.id:  # type: ignore
                result = cast(PredictionTask, self._result)
                return (self._response, result)
            raise RunnerBusyError()

        # Set up logger context for main thread. The same thing happens inside
        # the predict thread.
        structlog.contextvars.bind_contextvars(prediction_id=request.id)

        # if upload url was not set, we can respect output_file_prefix
        # but maybe we should just throw an error
        upload_url = request.output_file_prefix or self._upload_url
        event_handler = PredictionEventHandler(request, self.client_manager, upload_url)
        self._response = event_handler.response

        prediction_input = PredictionInput.from_request(request)

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
                event_stream = self._worker.predict(prediction_input.payload, poll=0.1)
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
                # FIXME: use isinstance(BaseInput)
                if hasattr(request.input, "cleanup"):
                    request.input.cleanup()  # type: ignore

        # this is still a little silly
        self._result = asyncio.create_task(async_predict_handling_errors())
        self._result.add_done_callback(self.make_error_handler("prediction"))
        # even after inlining we might still need a callback to surface remaining exceptions/results
        return (self._response, self._result)

    def is_busy(self) -> bool:
        if self._result is None:
            return False

        if not self._result.done():
            return True

        self._response = None
        self._result = None
        return False

    def shutdown(self) -> None:
        if self._result:
            self._result.cancel()
        self._worker.terminate()

    def cancel(self, prediction_id: Optional[str] = None) -> None:
        if not self.is_busy():
            return
        assert self._response is not None
        if prediction_id is not None and prediction_id != self._response.id:
            raise UnknownPredictionError()
        self._worker.cancel()


class PredictionEventHandler:
    def __init__(
        self,
        request: schema.PredictionRequest,
        client_manager: ClientManager,
        upload_url: Optional[str],
    ) -> None:
        log.info("starting prediction")
        # maybe this should be a deep copy to not share File state with child worker
        self.p = schema.PredictionResponse(**request.dict())
        self.p.status = schema.Status.PROCESSING
        self.p.output = None
        self.p.logs = ""
        self.p.started_at = datetime.now(tz=timezone.utc)

        self._client_manager = client_manager
        self._webhook_sender = client_manager.make_webhook_sender(
            request.webhook,
            request.webhook_events_filter or schema.WebhookEvent.default_events(),
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
            return self.noop()
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
        return self.noop()


async def setup(*, worker: Worker) -> SetupResult:
    logs = []
    status = None
    started_at = datetime.now(tz=timezone.utc)

    try:
        async for event in worker.setup():
            if isinstance(event, Log):
                logs.append(event.message)
            elif isinstance(event, Done):
                status = (
                    schema.Status.FAILED if event.error else schema.Status.SUCCEEDED
                )
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
