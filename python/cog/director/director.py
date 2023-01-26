import json
import queue
import signal
import time
from typing import Any, Callable, Dict, List, Optional

import requests
import structlog
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry  # type: ignore

from .. import schema
from ..server.probes import ProbeHelper
from ..server.webhook import requests_session, webhook_caller
from .eventtypes import Health, HealthcheckStatus, Webhook
from .healthchecker import Healthchecker
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
        redis_consumer_rotator: RedisConsumerRotator,
        predict_timeout: int,
        max_failure_count: int,
        report_setup_run_url: str,
    ):
        self.events = events
        self.healthchecker = healthchecker
        self.redis_consumer_rotator = redis_consumer_rotator
        self.predict_timeout = predict_timeout
        self.max_failure_count = max_failure_count
        self.report_setup_run_url = report_setup_run_url

        self._failure_count = 0
        self._should_exit = False
        self._shutdown_hooks: List[Callable] = []

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
                    "waiting for model container to complete setup",
                    wait_seconds=wait_seconds,
                )
                continue

            if not isinstance(event, HealthcheckStatus):
                log.warn(
                    "received unexpected event while waiting for setup", data=event
                )
                continue

            if event.health == Health.UNKNOWN:
                log.warn(
                    "received unexpected event while waiting for setup", data=event
                )
                continue

            wait_seconds = time.perf_counter() - mark
            log.info(
                "model container finished setup",
                wait_seconds=wait_seconds,
                health=event.health,
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

            self._confirm_model_health()

            try:
                message_id, message_json = redis_consumer.get()
            except EmptyRedisStream:
                continue
            except Exception:
                self._record_failure()
                log.error("error fetching message from redis", exc_info=True)
                time.sleep(5)
                continue

            structlog.contextvars.bind_contextvars(message_id=message_id)

            try:
                log.info("received message")
                self._handle_message(message_id, message_json)
            except Exception:
                self._record_failure()
                log.error("caught exception while running prediction", exc_info=True)
            finally:
                # See the comment in RedisConsumer.get to understand why we ack
                # even when an exception is thrown while handling a message.
                redis_consumer.ack(message_id)
                log.info("acked message")

    def _handle_message(self, message_id: str, message_json: str) -> None:
        message = json.loads(message_json)
        prediction_id = message["id"]

        structlog.contextvars.bind_contextvars(
            prediction_id=prediction_id,
            model_version=message["version"],
        )

        log.info("running prediction")
        should_cancel = self.redis_consumer_rotator.get_current().checker(
            message.get("cancel_key")
        )

        # Tracker is tied to a single prediction, and deliberately only exists
        # within this method in an attempt to eliminate the possibility that we
        # mix up state between predictions.
        tracker = PredictionTracker(
            response=schema.PredictionResponse(**message),
            webhook_caller=webhook_caller(message["webhook"]),
        )

        # Override webhook to call us
        message["webhook"] = "http://localhost:4900/webhook"

        # Call the model container to start the prediction
        try:
            resp = self.cog_client.post(
                self.cog_http_base + "/predictions",
                json=message,
                headers={"Prefer": "respond-async"},
                timeout=PREDICTION_CREATE_TIMEOUT,
            )
        except requests.exceptions.RequestException:
            tracker.fail("Unknown error handling prediction.")
            log.error("prediction failed: could not create prediction", exc_info=True)
            self._record_failure()
            return

        try:
            resp.raise_for_status()
        except requests.exceptions.RequestException:
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
            self._record_failure()
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
                    if event.health != Health.HEALTHY:
                        tracker.fail("Model stopped responding during prediction.")
                        self._abort(
                            "prediction failed: model container failed healthchecks",
                            health=event.health.name,
                        )
                else:
                    log.warn("received unknown event", data=event)

            if should_cancel():
                log.info("prediction cancelation requested")
                self._cancel_prediction(prediction_id)
                break

            if self.predict_timeout and tracker.runtime > self.predict_timeout:
                log.warn(
                    "prediction cancelation requested due to timeout",
                    predict_timeout=self.predict_timeout,
                )
                # Mark the prediction as timed out so we handle the
                # cancelation webhook appropriately.
                tracker.timed_out()
                self._cancel_prediction(prediction_id)
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
            self._abort("prediction failed to complete after cancelation")

        # Keep track of runs of failures to catch the situation where the
        # worker has gotten into a bad state where it can only fail
        # predictions, but isn't exiting.
        if tracker.status == schema.Status.FAILED:
            log.warn("prediction failed")
            self._record_failure()
        elif tracker.status == schema.Status.CANCELED:
            log.info("prediction canceled")
            self._record_success()
        else:
            log.info("prediction succeeded")
            self._record_success()

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

            if event.health == Health.HEALTHY:
                return
            else:
                self._abort("healthcheck confirmation: model container is not healthy")

        self._abort(
            "healthcheck confirmation: waited too long without response",
            wait_seconds=HEALTHCHECK_WAIT,
        )

    def _cancel_prediction(self, prediction_id: Any) -> None:
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

    def _record_failure(self) -> None:
        if not self.max_failure_count:
            return
        self._failure_count += 1
        if self._failure_count > self.max_failure_count:
            self._abort(
                "saw too many failures in a row", failure_count=self._failure_count
            )

    def _record_success(self) -> None:
        self._failure_count = 0

    def _abort(self, message: str, **kwds: Any) -> None:
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
