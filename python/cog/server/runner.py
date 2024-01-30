import asyncio
import threading
import traceback
import typing  # TypeAlias, py3.10
from datetime import datetime, timezone
from typing import Any, AsyncIterator, Awaitable, Callable, Optional, Union, cast

import httpx
import structlog
from attrs import define
from fastapi.encoders import jsonable_encoder

from .. import schema
from ..files import httpx_file_client, make_file_uploader
from . import webhooks
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
from .worker import InvalidStateException, Worker

log = structlog.get_logger("cog.server.runner")


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
        concurrency: int = 1,
    ) -> None:
        self._worker = Worker(predictor_ref=predictor_ref, concurrency=concurrency)
        self._should_cancel = asyncio.Event()

        self._shutdown_event = shutdown_event
        self._upload_url = upload_url
        self._predictions: "dict[str, tuple[schema.PredictionResponse, PredictionTask]]" = (
            {}
        )
        self._webhook_client = webhooks.httpx_client()
        self._retry_webhook_client = webhooks.httpx_retry_client()
        self._file_client = httpx_file_client()

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
        async def wrap_error() -> SetupResult:
            try:
                return await setup(worker=self._worker)
            except InvalidStateException as e:
                raise RunnerBusyError() from e

        result = asyncio.create_task(wrap_error())
        result.add_done_callback(self.make_error_handler("setup"))
        return result

    def create_event_handler(
        self, prediction: schema.PredictionRequest, upload_url: Optional[str]
    ) -> "PredictionEventHandler":
        response = schema.PredictionResponse(**prediction.dict())

        webhook = prediction.webhook
        events_filter = (
            prediction.webhook_events_filter or schema.WebhookEvent.default_events()
        )

        webhook_sender = None
        if webhook is not None:
            webhook_sender = webhooks.filtered_caller(
                webhook,
                set(events_filter),
                self._webhook_client,
                self._retry_webhook_client,
            )

        file_uploader: Optional[Callable[[Any], Any]] = None
        if upload_url is not None:
            file_uploader = make_file_uploader(self._file_client, upload_url)

        event_handler = PredictionEventHandler(
            response, webhook_sender=webhook_sender, file_uploader=file_uploader
        )

        return event_handler

    # TODO: Make the return type AsyncResult[schema.PredictionResponse] when we
    # no longer have to support Python 3.8
    def predict(
        self, prediction: schema.PredictionRequest, upload: bool = True
    ) -> "tuple[schema.PredictionResponse, PredictionTask]":
        if self.is_busy():
            if prediction.id in self._predictions:
                return self._predictions[prediction.id]
            raise RunnerBusyError()

        # Set up logger context for main thread. The same thing happens inside
        # the predict thread.
        structlog.contextvars.clear_contextvars()
        structlog.contextvars.bind_contextvars(prediction_id=prediction.id)

        self._should_cancel.clear()
        upload_url = self._upload_url if upload else None
        # this is supposed to send START, but we're trapped in a sync function
        event_handler = self.create_event_handler(prediction, upload_url=upload_url)

        def handle_cleanup(_: PredictionTask) -> None:
            input = cast(Any, prediction.input)
            if hasattr(input, "cleanup"):
                input.cleanup()
            self._predictions.pop(prediction.id)  # this might be too early

        response = event_handler.response
        self._worker.eager_predict_state_change(prediction.id)
        coro = predict_and_handle_errors(
            worker=self._worker,
            request=prediction,
            event_handler=event_handler,
            should_cancel=self._should_cancel,
        )
        # this is a little bit silly because we're making a sync handle
        # on a sync function that also wraps a future
        result = asyncio.create_task(coro)
        result.add_done_callback(handle_cleanup)
        result.add_done_callback(self.make_error_handler("prediction"))
        self._predictions[prediction.id] = (response, result)

        return (response, result)

    def is_busy(self) -> bool:
        return self._worker.is_busy()

    def shutdown(self) -> None:
        for _, task in self._predictions.values():
            task.cancel()
        self._worker.terminate()

    def cancel(self, prediction_id: str) -> None:
        try:
            self._worker.cancel(prediction_id)
            # if the runner is in an invalid state, predictions_in_flight would just be empty
            # and it's a keyerror anyway
        except KeyError as e:
            print(e)
            raise UnknownPredictionError() from e


class PredictionEventHandler:
    def __init__(
        self,
        p: schema.PredictionResponse,
        webhook_sender: Optional[webhooks.WebhookSenderType] = None,
        file_uploader: Optional[Callable[[Any], Awaitable[Any]]] = None,
    ) -> None:
        log.info("starting prediction")
        self.p = p
        self.p.status = schema.Status.PROCESSING
        self.p.output = None
        self.p.logs = ""
        self.p.started_at = datetime.now(tz=timezone.utc)

        self._webhook_sender = webhook_sender
        self._file_uploader = file_uploader

        # HACK: don't send an initial webhook if we're trying to optimize for
        # latency (this guarantees that the first output webhook won't be
        # throttled.)
        if not webhooks.SKIP_START_EVENT:
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
        if self._webhook_sender is not None:
            dict_response = jsonable_encoder(self.response.dict(exclude_unset=True))
            self._webhook_sender(dict_response, event)

    async def _upload_files(self, output: Any) -> Any:
        if self._file_uploader is None:
            return output
        try:
            # TODO: clean up output files
            return self._file_uploader(output)
        except Exception as error:
            # If something goes wrong uploading a file, it's irrecoverable.
            # The re-raised exception will be caught and cause the prediction
            # to be failed, with a useful error message.
            raise FileUploadError("Got error trying to upload output files") from error

    async def handle_event_stream(
        self, events: AsyncIterator[PublicEventType]
    ) -> schema.PredictionResponse:
        output_type = None
        async for event in events:
            if isinstance(event, Heartbeat):
                # Heartbeat events exist solely to ensure that we have a
                # regular opportunity to check for cancelation and
                # timeouts.
                #
                # We don't need to do anything with them.
                pass

            elif isinstance(event, Log):
                await self.append_logs(event.message)

            elif isinstance(event, PredictionOutputType):
                if output_type is not None:
                    await self.failed(error="Predictor returned unexpected output")
                    break

                output_type = event
                if output_type.multi:
                    await self.set_output([])
            elif isinstance(event, PredictionOutput):
                if output_type is None:
                    await self.failed(error="Predictor returned unexpected output")
                    break

                if output_type.multi:
                    await self.append_output(event.payload)
                else:
                    await self.set_output(event.payload)

            elif isinstance(event, Done):  # pyright: ignore reportUnnecessaryIsinstance
                if event.canceled:
                    await self.canceled()
                elif event.error:
                    await self.failed(error=str(event.error_detail))
                else:
                    await self.succeeded()

            else:  # shouldn't happen, exhausted the type
                log.warn("received unexpected event from worker", data=event)
        return self.response


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


async def predict_and_handle_errors(
    *,
    worker: Worker,
    request: schema.PredictionRequest,
    event_handler: PredictionEventHandler,
    should_cancel: asyncio.Event,
) -> schema.PredictionResponse:
    # Set up logger context within prediction thread.
    structlog.contextvars.clear_contextvars()
    structlog.contextvars.bind_contextvars(prediction_id=request.id)

    try:
        prediction_input = PredictionInput.from_request(request)
        predict_events = worker.predict(prediction_input, poll=0.1, eager=False)
        return await event_handler.handle_event_stream(predict_events)
    except httpx.RequestError as e:
        tb = traceback.format_exc()
        await event_handler.append_logs(tb)
        await event_handler.failed(error=str(e))
        log.warn("failed to download url path from input", exc_info=True)
        return event_handler.response
    except Exception as e:
        tb = traceback.format_exc()
        await event_handler.append_logs(tb)
        await event_handler.failed(error=str(e))
        raise
