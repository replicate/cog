import asyncio
import io
import threading
import traceback
import uuid
from abc import ABC, abstractmethod
from concurrent.futures import Future
from datetime import datetime, timezone
from typing import Any, Callable, Dict, Generic, List, Literal, Optional, TypeVar, Union

import requests
import structlog
from attrs import define, field
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

from .. import schema
from ..base_input import BaseInput
from ..files import put_file_to_signed_endpoint
from ..json import upload_files
from ..types import PYDANTIC_V2
from .errors import FileUploadError, RunnerBusyError, UnknownPredictionError
from .eventtypes import (
    Done,
    Log,
    PredictionMetric,
    PredictionOutput,
    PredictionOutputType,
)

if PYDANTIC_V2:
    from .helpers import unwrap_pydantic_serialization_iterators
from .telemetry import current_trace_context
from .useragent import get_user_agent
from .webhook import SKIP_START_EVENT, webhook_caller_filtered
from .worker import Worker, _PublicEventType

log = structlog.get_logger("cog.server.runner")


@define
class SetupResult:
    started_at: datetime
    completed_at: Optional[datetime] = None
    logs: List[str] = field(factory=list)
    status: Optional[Literal[schema.Status.FAILED, schema.Status.SUCCEEDED]] = None

    def to_dict(self) -> Dict[str, Any]:
        return {
            "started_at": self.started_at,
            "completed_at": self.completed_at,
            "logs": "".join(self.logs),
            "status": self.status,
        }


class PredictionRunner:
    """
    PredictionRunner manages the state of predictions running through the
    passed worker.
    """

    def __init__(
        self,
        *,
        max_concurrency: int = 1,
        worker: Worker,
    ) -> None:
        self._worker = worker
        self._max_concurrency = max_concurrency

        self._setup_task: Optional[SetupTask] = None
        self._predict_tasks: Dict[str, PredictTask] = {}
        self._predict_tasks_lock = threading.Lock()

    def setup(self) -> "SetupTask":
        assert self._setup_task is None, "do not call setup twice"

        self._setup_task = SetupTask()

        sid = self._worker.subscribe(self._setup_task.handle_event)
        self._setup_task.track(self._worker.setup())
        self._setup_task.add_done_callback(lambda _: self._worker.unsubscribe(sid))

        return self._setup_task

    def predict(
        self,
        prediction: schema.PredictionRequest,
        is_train: bool,
        task_kwargs: Optional[Dict[str, Any]] = None,
    ) -> "PredictTask":
        self._raise_if_busy()

        task_kwargs = task_kwargs or {}

        tag = prediction.id
        if tag is None:
            tag = uuid.uuid4().hex

        task = PredictTask(prediction, is_train, **task_kwargs)

        with self._predict_tasks_lock:
            self._predict_tasks[tag] = task

        if isinstance(prediction.input, BaseInput):
            if PYDANTIC_V2:
                payload = unwrap_pydantic_serialization_iterators(
                    prediction.input.model_dump()
                )
            else:
                payload = prediction.input.dict()
        else:
            payload = prediction.input.copy()

        if prediction.context is None:
            prediction.context = {}
        if prediction.id is not None:
            prediction.context["id"] = prediction.id

        sid = self._worker.subscribe(task.handle_event, tag=tag)
        task.track(self._worker.predict(payload, context=prediction.context, tag=tag))
        task.add_done_callback(self._task_done_callback(tag, sid))

        return task

    def _task_done_callback(self, tag: str, sid: int) -> Callable[[Any], None]:
        def _callback(_) -> None:
            self._worker.unsubscribe(sid)
            with self._predict_tasks_lock:
                del self._predict_tasks[tag]

        return _callback

    def get_predict_task(self, id: str) -> Optional["PredictTask"]:
        with self._predict_tasks_lock:
            return self._predict_tasks.get(id, None)

    def is_busy(self) -> bool:
        try:
            self._raise_if_busy()
        except RunnerBusyError:
            return True
        return False

    def cancel(self, prediction_id: str) -> None:
        if not prediction_id:
            raise ValueError("prediction_id is required")
        with self._predict_tasks_lock:
            if (
                prediction_id not in self._predict_tasks
                or self._predict_tasks[prediction_id].done()
            ):
                raise UnknownPredictionError("unknown prediction id")
        self._worker.cancel(tag=prediction_id)

    def _raise_if_busy(self) -> None:
        if self._setup_task is None:
            # Setup hasn't been called yet.
            raise RunnerBusyError("setup has not started")
        if not self._setup_task.done():
            # Setup is still running.
            raise RunnerBusyError("setup is not complete")

        with self._predict_tasks_lock:
            processing_tasks = [
                id for id in self._predict_tasks if not self._predict_tasks[id].done()
            ]

        if len(processing_tasks) >= self._max_concurrency:
            # We're at max concurrency
            if self._max_concurrency == 1:
                raise RunnerBusyError("prediction running")
            raise RunnerBusyError("max predictions running")


T = TypeVar("T")


class Task(ABC, Generic[T]):
    @abstractmethod
    def track(self, fut: "Future[Done]") -> None:
        raise NotImplementedError

    @abstractmethod
    def add_done_callback(self, fn: Callable[[T], None]) -> None:
        raise NotImplementedError

    @abstractmethod
    def done(self) -> bool:
        raise NotImplementedError

    @abstractmethod
    def wait(self, timeout: Optional[float] = None) -> None:
        raise NotImplementedError

    @property
    @abstractmethod
    def result(self) -> T:
        raise NotImplementedError


class SetupTask(Task[SetupResult]):
    def __init__(self, _clock: Optional[Callable[[], datetime]] = None) -> None:
        log.info("starting setup")
        self._clock = _clock
        if self._clock is None:
            self._clock = lambda: datetime.now(timezone.utc)

        self._fut: "Optional[Future[Done]]" = None
        self._result = SetupResult(started_at=self._clock())

    @property
    def result(self) -> SetupResult:
        return self._result

    def track(self, fut: "Future[Done]") -> None:
        log.info("started setup")
        self._fut = fut
        self._fut.add_done_callback(self._handle_done)

    def add_done_callback(self, fn: Callable[[SetupResult], None]) -> None:
        assert self._fut, "call track before adding callbacks"
        self._fut.add_done_callback(lambda _: fn(self.result))

    def done(self) -> bool:
        assert self._fut, "call track before checking done"
        return self._fut.done()

    def wait(self, timeout: Optional[float] = None) -> None:
        assert self._fut, "call track before waiting"
        self._fut.result(timeout=timeout)

    def append_logs(self, message: str) -> None:
        self._result.logs.append(message)

    def succeeded(self) -> None:
        log.info("setup succeeded")
        assert self._clock
        self._result.completed_at = self._clock()
        self._result.status = schema.Status.SUCCEEDED

    def failed(self) -> None:
        log.info("setup failed")
        assert self._clock
        self._result.completed_at = self._clock()
        self._result.status = schema.Status.FAILED

    def handle_event(self, event: _PublicEventType) -> None:
        if isinstance(event, Log):
            self.append_logs(event.message)
        elif isinstance(event, Done):
            if event.error:
                self.failed()
            else:
                self.succeeded()
        else:
            log.warn("received unexpected event during setup", data=event)

    def _handle_done(self, f: "Future[Done]") -> None:
        try:
            # See if the future captured an exception...
            f.result()
        except Exception:  # pylint: disable=broad-exception-caught
            log.error("caught exception while running setup", exc_info=True)
            self.append_logs(traceback.format_exc())
            self.failed()


def generate_file_uploader(
    upload_url: str, prediction_id: Optional[str]
) -> Callable[[Any], Any]:
    client = _make_file_upload_http_client()

    def file_uploader(output: Any) -> Any:
        def upload_file(fh: io.IOBase) -> str:
            return put_file_to_signed_endpoint(
                fh, endpoint=upload_url, prediction_id=prediction_id, client=client
            )

        return upload_files(output, upload_file=upload_file)

    return file_uploader


class PredictTask(Task[schema.PredictionResponse]):
    def __init__(
        self,
        prediction_request: schema.PredictionRequest,
        is_train: bool,
        upload_url: Optional[str] = None,
    ) -> None:
        self._is_train = is_train
        self._log = log.bind(prediction_id=prediction_request.id)

        self._log.info("starting " + ("prediction" if not is_train else "train"))

        self._fut: "Optional[Future[Done]]" = None

        if PYDANTIC_V2:
            request_dict = unwrap_pydantic_serialization_iterators(
                prediction_request.model_dump()
            )
        else:
            request_dict = prediction_request.dict()

        self._p = schema.PredictionResponse(**request_dict)
        self._p.status = schema.Status.PROCESSING
        self._output_type_multi = None
        self._p.output = None
        self._p.logs = ""
        self._p.started_at = datetime.now(tz=timezone.utc)

        self._webhook_sender = None
        if prediction_request.webhook:
            self._webhook_sender = webhook_caller_filtered(
                str(prediction_request.webhook),
                set(
                    prediction_request.webhook_events_filter
                    or schema.WebhookEvent.default_events()
                ),
            )

        self._file_uploader = None
        if upload_url:
            self._file_uploader = generate_file_uploader(
                upload_url, prediction_id=self._p.id
            )

    @property
    def result(self) -> schema.PredictionResponse:
        return self._p

    def track(self, fut: "Future[Done]") -> None:
        self._log.info("started " + ("prediction" if not self._is_train else "train"))

        # HACK: don't send an initial webhook if we're trying to optimize for
        # latency (this guarantees that the first output webhook won't be
        # throttled.)
        if not SKIP_START_EVENT:
            self._send_webhook(schema.WebhookEvent.START)

        self._fut = fut
        self._fut.add_done_callback(self._handle_done)

    def add_done_callback(
        self, fn: Callable[[schema.PredictionResponse], None]
    ) -> None:
        assert self._fut, "call track before adding callbacks"
        self._fut.add_done_callback(lambda _: fn(self.result))

    def done(self) -> bool:
        assert self._fut, "call track before checking done"
        return self._fut.done()

    async def wait_async(self) -> None:
        assert self._fut, "call track before waiting"
        await asyncio.wrap_future(self._fut)
        return None

    def wait(self, timeout: Optional[float] = None) -> None:
        assert self._fut, "call track before waiting"
        self._fut.result(timeout=timeout)

    def set_output_type(self, *, multi: bool) -> None:
        assert self._output_type_multi is None, (
            "Predictor unexpectedly returned multiple output types"
        )
        assert self._p.output is None, (
            "Predictor unexpectedly returned output type after output"
        )

        if multi:
            self._p.output = []

        self._output_type_multi = multi

    def append_output(self, output: Any) -> None:
        assert self._output_type_multi is not None, (
            "Predictor unexpectedly returned output before output type"
        )

        uploaded_output = self._upload_files(output)
        if self._output_type_multi:
            self._p.output.append(uploaded_output)
            self._send_webhook(schema.WebhookEvent.OUTPUT)
        else:
            self._p.output = uploaded_output
            # We don't send a webhook for compatibility with the behaviour of
            # redis_queue. In future we can consider whether it makes sense to send
            # one here.

    def append_logs(self, logs: str) -> None:
        assert self._p.logs is not None
        self._p.logs += logs
        self._send_webhook(schema.WebhookEvent.LOGS)

    def set_metric(self, key: str, value: Union[float, int]) -> None:
        if self._p.metrics is None:
            self._p.metrics = {}
        self._p.metrics[key] = value

    def succeeded(self) -> None:
        self._log.info(("prediction" if not self._is_train else "train") + " succeeded")
        self._p.status = schema.Status.SUCCEEDED
        self._set_completed_at()
        # These have been set already: this is to convince the typechecker of
        # that...
        assert self._p.completed_at is not None
        assert self._p.started_at is not None
        self.set_metric(
            "predict_time",
            (self._p.completed_at - self._p.started_at).total_seconds(),
        )
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def failed(self, error: str) -> None:
        self._log.info("prediction failed", error=error)
        self._p.status = schema.Status.FAILED
        self._p.error = error
        self._set_completed_at()
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def canceled(self) -> None:
        self._log.info("prediction canceled")
        self._p.status = schema.Status.CANCELED
        self._set_completed_at()
        self._send_webhook(schema.WebhookEvent.COMPLETED)

    def handle_event(self, event: _PublicEventType) -> None:
        try:
            if isinstance(event, Log):
                self.append_logs(event.message)
            elif isinstance(event, PredictionMetric):
                self.set_metric(event.name, event.value)
            elif isinstance(event, PredictionOutputType):
                self.set_output_type(multi=event.multi)
            elif isinstance(event, PredictionOutput):
                self.append_output(event.payload)
            elif isinstance(event, Done):  # pyright: ignore reportUnnecessaryIsinstance
                if event.canceled:
                    self.canceled()
                elif event.error:
                    self.failed(error=str(event.error_detail))
                else:
                    self.succeeded()
            else:  # shouldn't happen, exhausted the type
                self._log.warn("received unexpected event during predict", data=event)
        except Exception as e:
            self.failed(str(e))

    def _set_completed_at(self) -> None:
        self._p.completed_at = datetime.now(tz=timezone.utc)

    def _send_webhook(self, event: schema.WebhookEvent) -> None:
        if self._webhook_sender is not None:
            self._webhook_sender(self._p, event)

    def _upload_files(self, output: Any) -> Any:
        if self._file_uploader is None:
            return output

        try:
            # TODO: clean up output files
            return self._file_uploader(output)
        except (FileNotFoundError, NotADirectoryError):
            # These error cases indicate that an output path returned by a prediction does
            # not actually exist, so there is no way for us to even attempt to upload it.
            # The error is re-raised without wrapping because this is not considered an
            # "infrastructure error", such as happens during an upload of a file that
            # **does** exist.
            raise
        except Exception as error:  # pylint: disable=broad-exception-caught
            # Any other errors that occur during file upload are irrecoverable and
            # considered "infrastructure errors" because there is a high likelihood that
            # the error happened in a layer that is outside the control of the model.
            raise FileUploadError("Got error trying to upload output files") from error

    def _handle_done(self, f: "Future[Done]") -> None:
        try:
            # See if the future captured an exception...
            f.result()
        except Exception as e:  # pylint: disable=broad-exception-caught
            self._log.error("caught exception while running predict", exc_info=True)
            self.append_logs(traceback.format_exc())
            self.failed(error=str(e))
            self._p._fatal_exception = e


def _make_file_upload_http_client() -> requests.Session:
    session = requests.Session()
    session.headers["user-agent"] = (
        get_user_agent() + " " + str(session.headers["user-agent"])
    )

    ctx = current_trace_context() or {}
    for key, value in ctx.items():
        session.headers[key] = str(value)

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
