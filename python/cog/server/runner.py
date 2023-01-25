import io
import os
import signal
import sys
from datetime import datetime, timezone
from fastapi.encoders import jsonable_encoder
from multiprocessing import Event
from multiprocessing.pool import ThreadPool, AsyncResult
from typing import Any, Callable, Dict, Optional, Tuple

import requests
import structlog
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry

from .. import schema
from ..files import put_file_to_signed_endpoint
from ..json import upload_files
from .eventtypes import Done, Heartbeat, Log, PredictionOutput, PredictionOutputType
from .webhook import webhook_caller, webhook_caller_filtered
from .worker import Worker

log = structlog.get_logger("cog.server.runner")


class FileUploadError(Exception):
    pass


class PredictionRunner:
    def __init__(self, predictor_ref: str, upload_url: Optional[str] = None):
        self.current_prediction_id = None
        self._thread = None
        self._threadpool = ThreadPool(processes=1)

        self._result: Optional[AsyncResult] = None

        self._worker = Worker(predictor_ref=predictor_ref)
        self._should_cancel = Event()

        self._upload_url = upload_url

    def setup(self) -> Tuple[schema.Status, str]:
        _logs = []
        _status = None

        try:
            for event in self._worker.setup():
                if isinstance(event, Log):
                    _logs.append(event.message)
                elif isinstance(event, Done):
                    _status = (
                        schema.Status.FAILED if event.error else schema.Status.SUCCEEDED
                    )
        except Exception:
            _status = schema.Status.FAILED

        assert _status is not None, "must receive done event from setup"

        return (_status, "".join(_logs))

    # TODO: Make the return type AsyncResult[schema.PredictionResponse] when we
    # no longer have to support Python 3.8
    def predict(self, prediction: schema.PredictionRequest) -> AsyncResult:
        # It's the caller's responsibility to not call us if we're busy.
        assert not self.is_busy()

        # Set up logger context for main thread. The same thing happens inside
        # the predict thread.
        structlog.contextvars.clear_contextvars()
        structlog.contextvars.bind_contextvars(prediction_id=prediction.id)

        self._should_cancel.clear()
        event_handler = create_event_handler(prediction, upload_url=self._upload_url)

        def cleanup(_=None):
            if hasattr(prediction.input, "cleanup"):
                prediction.input.cleanup()

        def handle_error(error):
            log.error("async predict thread exited with an error", error=error)
            cleanup()

            # Crash the container -- we don't have the context to recover
            # gracefully from here.
            #
            # This function gets called in a thread, and there are least two
            # more active threads at this point, so there's no easy way to
            # cleanly coordinate a shutdown.
            #
            # This approach is a little bit overkill, and probably wants
            # revisiting when we get a chance.
            os.kill(os.getpid(), signal.SIGKILL)

        self._result = self._threadpool.apply_async(
            func=predict,
            args=(self._worker, prediction, self._should_cancel, event_handler),
            callback=cleanup,
            error_callback=handle_error,
        )

        self.current_prediction_id = prediction.id
        return (event_handler.response, self._result)

    def is_busy(self) -> bool:
        if self._result is None:
            return False

        if not self._result.ready():
            return True

        self._result = None
        return False

    def shutdown(self) -> None:
        self._worker.terminate()
        self._threadpool.terminate()
        self._threadpool.join()

    def cancel(self) -> None:
        self._should_cancel.set()


def create_event_handler(prediction, upload_url: Optional[str] = None):
    response = schema.PredictionResponse(**prediction.dict())

    webhook = prediction.webhook
    events_filter = (
        prediction.webhook_events_filter or schema.WebhookEvent.default_events()
    )

    webhook_sender = None
    if webhook is not None:
        webhook_sender = webhook_caller_filtered(webhook, events_filter)

    file_uploader = None
    if upload_url is not None:
        file_uploader = generate_file_uploader(upload_url)

    event_handler = PredictionEventHandler(
        response, webhook_sender=webhook_sender, file_uploader=file_uploader
    )

    return event_handler


def generate_file_uploader(upload_url):
    client = _make_file_upload_http_client()

    def file_uploader(output):
        def upload_file(fh: io.IOBase) -> str:
            return put_file_to_signed_endpoint(fh, upload_url, client=client)

        return upload_files(output, upload_file=upload_file)

    return file_uploader


class PredictionEventHandler:
    def __init__(
        self,
        p: schema.PredictionResponse,
        webhook_sender: Optional[Callable] = None,
        file_uploader: Optional[Callable] = None,
    ):
        log.info("starting prediction")
        self.p = p
        self.p.status = schema.Status.PROCESSING
        self.p.output = None
        self.p.logs = ""
        self.p.started_at = datetime.now(tz=timezone.utc)

        self._webhook_sender = webhook_sender
        self._file_uploader = file_uploader

        self._send_webhook(schema.WebhookEvent.START)

    @property
    def response(self):
        return self.p

    def set_output(self, output: Any) -> None:
        assert self.p.output is None, "Predictor unexpectedly returned multiple outputs"
        self.p.output = self._upload_files(output)
        # We don't send a webhook for compatibility with the behaviour of
        # redis_queue. In future we can consider whether it makes sense to send
        # one here.

    def append_output(self, output: Any) -> None:
        assert isinstance(
            self.p.output, list
        ), "Cannot append output before setting output"
        self.p.output.append(self._upload_files(output))
        self._send_webhook(schema.WebhookEvent.OUTPUT)

    def append_logs(self, logs: str) -> None:
        assert self.p.logs is not None
        self.p.logs += logs
        self._send_webhook(schema.WebhookEvent.LOGS)

    def succeeded(self) -> None:
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
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def failed(self, error: str) -> None:
        log.info("prediction failed", error=error)
        self.p.status = schema.Status.FAILED
        self.p.error = error
        self._set_completed_at()
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def canceled(self) -> None:
        log.info("prediction canceled")
        self.p.status = schema.Status.CANCELED
        self._set_completed_at()
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def _set_completed_at(self) -> None:
        self.p.completed_at = datetime.now(tz=timezone.utc)

    def _send_webhook(self, event: schema.WebhookEvent) -> None:
        if self._webhook_sender is not None:
            dict_response = jsonable_encoder(self.response.dict())
            self._webhook_sender(dict_response, event)

    def _upload_files(self, output: Any) -> Any:
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


def predict(
    worker: Worker,
    request: schema.PredictionRequest,
    should_cancel: Event,
    event_handler: PredictionEventHandler,
) -> schema.PredictionResponse:

    # Set up logger context within prediction thread.
    structlog.contextvars.clear_contextvars()
    structlog.contextvars.bind_contextvars(prediction_id=request.id)

    try:
        return _predict(worker, request, should_cancel, event_handler)
    except Exception as error:
        log.error("caught exception while running prediction", exc_info=True)

        # Attempt to fail the prediction and trigger a webhook, but if that
        # also fails just move on
        try:
            # TODO: include a stack trace
            event_handler.failed(
                error=f"Got unexpected error during prediction response handling: {error}"
            )
        except Exception as another_error:
            log.error(
                "caught exception while handling prediction failure", exc_info=True
            )
        # Any error encountered like this is likely irrecoverable; re-raise the
        # error to cause the container to crash.
        raise error


def _predict(
    worker: Worker,
    request: schema.PredictionRequest,
    should_cancel: Event,
    event_handler: PredictionEventHandler,
) -> schema.PredictionResponse:
    initial_prediction = request.dict()

    output_type = None
    for event in worker.predict(initial_prediction["input"], poll=0.1):
        if should_cancel.is_set():
            worker.cancel()
            should_cancel.clear()

        if isinstance(event, Heartbeat):
            # Heartbeat events exist solely to ensure that we have a
            # regular opportunity to check for cancelation and
            # timeouts.
            #
            # We don't need to do anything with them.
            pass

        elif isinstance(event, Log):
            event_handler.append_logs(event.message)

        elif isinstance(event, PredictionOutputType):
            if output_type is not None:
                event_handler.failed(error="Predictor returned unexpected output")
                break

            output_type = event
            if output_type.multi:
                event_handler.set_output([])
        elif isinstance(event, PredictionOutput):
            if output_type is None:
                event_handler.failed(error="Predictor returned unexpected output")
                break

            if output_type.multi:
                event_handler.append_output(event.payload)
            else:
                event_handler.set_output(event.payload)

        elif isinstance(event, Done):
            if event.canceled:
                event_handler.canceled()
            elif event.error:
                event_handler.failed(error=str(event.error_detail))
            else:
                event_handler.succeeded()

        else:
            log.warn("received unexpected event from worker", data=event)

    return event_handler.response


def _make_file_upload_http_client() -> requests.Session:
    session = requests.Session()
    adapter = HTTPAdapter(
        max_retries=Retry(
            total=3,
            backoff_factor=0.1,
            status_forcelist=[408, 429, 500, 502, 503, 504],
            allowed_methods=["PUT"],
        ),
    )
    session.mount("http://", adapter)
    session.mount("https://", adapter)
    return session
