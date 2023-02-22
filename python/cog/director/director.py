import datetime
import json
import queue
import signal
import time
from typing import Any, Callable, Dict, List, Optional

import requests
import structlog
from opentelemetry import trace
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry  # type: ignore

from .. import schema
from ..server.http import Health
from ..server.probes import ProbeHelper
from ..server.webhook import requests_session, webhook_caller
from .eventtypes import HealthcheckStatus, Webhook
from .healthchecker import Healthchecker
from .monitor import Monitor, span_attributes_from_env
from .prediction_tracker import PredictionTracker
from .redis import EmptyRedisStream, RedisConsumerRotator

log = structlog.get_logger(__name__)

# How often to check for cancelation or shutdown signals while a prediction is
# running, in seconds. 100ms mirrors the value currently supplied to the `poll`
# keyword argument for Worker.predict(...) in the redis queue worker code.
POLL_INTERVAL = 0.1

# How long it's acceptable to wait for a prediction create request to respond,
# in seconds.
PREDICTION_CREATE_TIMEOUT = 5

# How long to wait for a cancelation to complete, in seconds.
# TODO: when the model container is no longer responsible for e.g. file
# uploads, we should likely try and reduce this to a smaller number, e.g. 5s.
CANCEL_WAIT = 30

# How long to wait for an explicit healthcheck request before aborting. Note
# that to avoid failing prematurely this should be at least as long as the it
# takes for the complete chain of retries configured for the Healthchecker.
HEALTHCHECK_WAIT = 10


class Abort(Exception):
    pass


class Director:
    def __init__(
        self,
        events: queue.Queue,
        healthchecker: Healthchecker,
        monitor: Monitor,
        redis_consumer_rotator: RedisConsumerRotator,
        predict_timeout: int,
        max_failure_count: int,
        report_setup_run_url: str,
    ):
        self.events = events
        self.healthchecker = healthchecker
        self.monitor = monitor
        self.redis_consumer_rotator = redis_consumer_rotator
        self.predict_timeout = predict_timeout
        self.max_failure_count = max_failure_count
        self.report_setup_run_url = report_setup_run_url

        self._failure_count = 0
        self._should_exit = False
        self._shutdown_hooks: List[Callable] = []
        self._tracer = trace.get_tracer("cog-director")

        self.cog_client = _make_local_http_client()
        self.cog_http_base = "http://localhost:5000"

    def start(self) -> None:
        try:
            signal.signal(signal.SIGINT, self._handle_exit)
            signal.signal(signal.SIGTERM, self._handle_exit)

            # Signal pod readiness (when in k8s)
            probes = ProbeHelper()
            probes.ready()

            # First, we wait for the model container to report a successful
            # setup.
            self._setup()

            # Now, we enter the main loop, pulling prediction requests from Redis
            # and managing the model container.
            self._loop()

        finally:
            log.info("shutting down worker: bye bye!")

            try:
                self._shutdown_model()
            except Exception:
                log.error("caught exception while shutting down model", exc_info=True)

            for hook in self._shutdown_hooks:
                try:
                    hook()
                except Exception:
                    log.error(
                        "caught exception while running shutdown hook", exc_info=True
                    )

    def register_shutdown_hook(self, hook: Callable) -> None:
        self._shutdown_hooks.append(hook)

    def _handle_exit(self, signum: Any, frame: Any) -> None:
        log.warn("received termination signal", signal=signal.Signals(signum).name)
        self._should_exit = True

    def _setup(self) -> None:
        mark = time.perf_counter()

        while not self._should_exit:
            try:
                event = self.events.get(timeout=1)
            except queue.Empty:
                wait_seconds = time.perf_counter() - mark
                log.info(
                    "setup: waiting for model container", wait_seconds=wait_seconds
                )
                continue

            if not isinstance(event, HealthcheckStatus):
                log.warn("setup: received unexpected event", data=event)
                continue

            if event.health not in {Health.READY, Health.SETUP_FAILED}:
                log.warn("setup: health status changed", health=event.health.name)
                continue

            wait_seconds = time.perf_counter() - mark
            log.info(
                "setup: model container finished setup",
                wait_seconds=wait_seconds,
                health=event.health.name,
            )
            self._report_setup_run(event.metadata)

            # if setup failed, exit immediately
            if event.health == Health.SETUP_FAILED:
                self._abort("model container failed setup")

            # Now that we've run setup, let's slow down healthchecks a little
            # bit. We don't need them to run every 100ms.
            self.healthchecker.set_interval(5)

            return

    def _loop(self) -> None:
        while not self._should_exit:
            self.redis_consumer_rotator.rotate()
            redis_consumer = self.redis_consumer_rotator.get_current()

            structlog.contextvars.clear_contextvars()
            structlog.contextvars.bind_contextvars(
                queue=redis_consumer.redis_input_queue,
            )
            self.monitor.set_current_prediction(None)

            self._confirm_model_health()

            try:
                message_json = redis_consumer.get()
            except EmptyRedisStream:
                continue
            except Exception:
                self._record_failure()
                log.error("error fetching message from redis", exc_info=True)
                time.sleep(5)
                continue

            structlog.contextvars.bind_contextvars(message_id=redis_consumer.message_id)

            try:
                log.info("received message")
                with self._tracer.start_as_current_span(
                    name="cog.prediction",
                    attributes=span_attributes_from_env(),
                ) as span:
                    self._handle_message(message_json, span)
            except Exception:
                self._record_failure()
                log.error("caught exception while running prediction", exc_info=True)
            finally:
                # See the comment in RedisConsumer.get to understand why we ack
                # even when an exception is thrown while handling a message.
                redis_consumer.ack()
                log.info("acked message")

    def _handle_message(self, message_json: str, span: trace.Span) -> None:
        message = json.loads(message_json)
        prediction_id = message["id"]

        structlog.contextvars.bind_contextvars(
            prediction_id=prediction_id,
            model_version=message["version"],
        )

        log.info("running prediction")

        # Tracker is tied to a single prediction, and deliberately only exists
        # within this method in an attempt to eliminate the possibility that we
        # mix up state between predictions.
        tracker = PredictionTracker(
            response=schema.PredictionResponse(**message),
            webhook_caller=webhook_caller(message["webhook"]),
        )
        self.monitor.set_current_prediction(tracker._response)
        self._set_span_attributes_from_tracker(span, tracker)

        should_cancel = self.redis_consumer_rotator.get_current().checker(
            message.get("cancel_key")
        )

        # Override webhook to call us
        message["webhook"] = "http://localhost:4900/webhook"

        # Call the model container to start the prediction
        try:
            resp = self.cog_client.put(
                self.cog_http_base + "/predictions/" + prediction_id,
                json=message,
                headers={"Prefer": "respond-async"},
                timeout=PREDICTION_CREATE_TIMEOUT,
            )
        except requests.exceptions.RequestException as e:
            tracker.fail("Unknown error handling prediction.")
            log.error("prediction failed: could not create prediction", exc_info=True)
            self._record_failure(span, e)
            return

        try:
            resp.raise_for_status()
        except requests.exceptions.RequestException as e:
            # Special case validation errors
            if resp.status_code == 422:
                tracker.fail(f"Prediction input failed validation: {resp.text}")
                log.warn(
                    "prediction failed: failed input validation",
                    status_code=resp.status_code,
                    response=resp.text,
                )
            else:
                tracker.fail("Unknown error handling prediction.")
                log.error(
                    "prediction failed: invalid response status from create request",
                    status_code=resp.status_code,
                    response=resp.text,
                )
            self._record_failure(span, e)
            return

        tracker.start()

        # Wait for any of: completion, shutdown signal. Also check to see if we
        # should cancel the running prediction, and make the appropriate HTTP
        # call if so.
        while not tracker.is_complete():
            try:
                event = self.events.get(timeout=POLL_INTERVAL)
            except queue.Empty:
                pass
            else:
                if isinstance(event, Webhook):
                    tracker.update_from_webhook_payload(event.payload)
                elif isinstance(event, HealthcheckStatus):
                    log.info("received healthcheck status update", data=event)
                    if event.health not in {Health.BUSY, Health.READY}:
                        tracker.fail("Model stopped responding during prediction.")
                        self._abort(
                            "prediction failed: model container failed healthchecks",
                            span=span,
                            health=event.health.name,
                        )
                else:
                    log.warn("received unknown event", data=event)

            if should_cancel():
                log.info("prediction cancelation requested")
                self._cancel_prediction(prediction_id, span)
                break

            if self.predict_timeout and tracker.runtime > self.predict_timeout:
                log.warn(
                    "prediction cancelation requested due to timeout",
                    predict_timeout=self.predict_timeout,
                )
                # Mark the prediction as timed out so we handle the
                # cancelation webhook appropriately.
                tracker.timed_out()
                self._cancel_prediction(prediction_id, span, timed_out=True)
                break

        # Wait up to another CANCEL_WAIT seconds for cancelation if necessary
        mark = time.perf_counter()
        while not tracker.is_complete() and time.perf_counter() - mark < CANCEL_WAIT:
            try:
                event = self.events.get(timeout=POLL_INTERVAL)
            except queue.Empty:
                pass
            else:
                if isinstance(event, Webhook):
                    tracker.update_from_webhook_payload(event.payload)

        # If the prediction is *still* not complete, something is badly wrong
        # and we should abort.
        if not tracker.is_complete():
            tracker.force_cancel()
            self._abort("prediction failed to complete after cancelation", span)

        # Keep track of runs of failures to catch the situation where the
        # worker has gotten into a bad state where it can only fail
        # predictions, but isn't exiting.
        if tracker.status == schema.Status.FAILED:
            log.warn("prediction failed")
            self._record_failure(span)
        elif tracker.status == schema.Status.CANCELED:
            log.info("prediction canceled")
            self._record_success()
        else:
            log.info("prediction succeeded")
            self._record_success()

        self._set_span_attributes_from_tracker(span, tracker)

    # OpenTelemetry is very picky about not accepting None types
    def _set_span_attributes_from_tracker(self, span, tracker):
        span.set_attribute("prediction.uuid", tracker._response.id)
        if tracker._response.error:
            span.set_attribute("prediction.error", tracker._response.error)
        if tracker._response.version:
            span.set_attribute("model.version", tracker._response.version)
        if tracker._response.started_at:
            span.set_attribute(
                "prediction.started_at", tracker._response.started_at.timestamp()
            )
        if tracker._response.created_at:
            span.set_attribute(
                "prediction.created_at", tracker._response.created_at.timestamp()
            )
        if tracker._response.status:
            span.set_attribute("prediction.status", tracker.status)
        if tracker._response.completed_at:
            span.set_attribute(
                "prediction.completed_at", tracker._response.completed_at.timestamp()
            )

    def _confirm_model_health(self) -> None:
        self.healthchecker.request_status()
        mark = time.perf_counter()

        while time.perf_counter() - mark < HEALTHCHECK_WAIT:
            try:
                event = self.events.get(timeout=POLL_INTERVAL)
            except queue.Empty:
                continue

            if not isinstance(event, HealthcheckStatus):
                log.warn(
                    "healthcheck confirmation: received unexpected event while waiting",
                    data=event,
                )
                continue

            if event.health == Health.READY:
                return

            # If we get anything else here: unknown, starting, busy,
            # setup_failed, it represents a loss of synchronization between
            # director and the model container, so we abort.
            self._abort(
                "healthcheck confirmation: model container is not healthy",
                health=event.health.name,
            )

        self._abort(
            "healthcheck confirmation: waited too long without response",
            wait_seconds=HEALTHCHECK_WAIT,
        )

    def _cancel_prediction(
        self, prediction_id: Any, span: trace.Span, timed_out=False
    ) -> None:
        span.set_attribute("prediction.status", "canceled")
        if timed_out:
            span.set_attribute("prediction.timed_out", True)

        resp = self.cog_client.post(
            self.cog_http_base + "/predictions/" + prediction_id + "/cancel",
            timeout=1,
        )
        resp.raise_for_status()

    def _report_setup_run(self, payload: Optional[Dict[str, Any]]) -> None:
        if not self.report_setup_run_url:
            return

        session = requests_session()
        try:
            # TODO this should be async so we can get on with predictions ASAP
            resp = session.post(self.report_setup_run_url, json=payload)
            resp.raise_for_status()
        except requests.exceptions.RequestException:
            log.warn("failed to report setup run", exc_info=True)

    def _record_failure(
        self,
        span: Optional[trace.Span] = None,
        exception: Optional[Exception] = None,
        response: Optional[requests.Response] = None,
    ) -> None:
        if span:
            span.set_attribute("prediction.status", "failed")
            if exception:
                span.record_exception(exception)
            if response:
                span.set_attribute("http.status_code", response.status_code)
                span.set_attribute("http.response_length", len(response.content))

        if not self.max_failure_count:
            return
        self._failure_count += 1
        if self._failure_count > self.max_failure_count:
            self._abort(
                "saw too many failures in a row",
                span=span,
                failure_count=self._failure_count,
            )

    def _record_success(self) -> None:
        self._failure_count = 0

    def _abort(
        self, message: str, span: Optional[trace.Span] = None, **kwds: Any
    ) -> None:
        if span:
            span.set_attribute("prediction.status", "failed")
            span.set_attribute("prediction.error_message", f"{message}: {str(kwds)}")

        self._should_exit = True
        log.error(message, **kwds)
        raise Abort(message)

    def _shutdown_model(self) -> None:
        resp = self.cog_client.post(
            self.cog_http_base + "/shutdown",
            timeout=1,
        )
        log.info("requested model container shutdown", response_code=resp.status_code)


def _make_local_http_client() -> requests.Session:
    session = requests.Session()
    adapter = HTTPAdapter(
        max_retries=Retry(
            total=3,
            backoff_factor=0.1,
            status_forcelist=[429, 500, 502, 503, 504],
            allowed_methods=["POST"],
        ),
    )
    session.mount("http://", adapter)
    session.mount("https://", adapter)
    return session
