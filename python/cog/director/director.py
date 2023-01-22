import datetime
import json
import logging
import os
import queue
import signal
import sys
import threading
import time
import traceback
from argparse import ArgumentParser
from typing import Any, Callable, Dict, Optional, Tuple

import requests
import structlog
import uvicorn
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry

from .. import schema
from ..server.probes import ProbeHelper
from ..server.webhook import requests_session, webhook_caller
from .eventtypes import Webhook
from .http import Server, create_app
from .prediction_tracker import PredictionTracker
from .redis import EmptyRedisStream, RedisConsumer

log = structlog.get_logger(__name__)

# How often to check for model container setup on boot.
SETUP_POLL_INTERVAL = 0.1

# How often to check for cancelation or shutdown signals while a prediction is
# running, in seconds. 100ms mirrors the value currently supplied to the `poll`
# keyword argument for Worker.predict(...) in the redis queue worker code.
POLL_INTERVAL = 0.1

# How long to wait for a cancelation to complete, in seconds.
CANCEL_WAIT = 5


class Abort(Exception):
    pass


class Director:
    def __init__(
        self,
        events: queue.Queue,
        redis_consumer: RedisConsumer,
        predict_timeout: int,
        max_failure_count: int,
        report_setup_run_url: str,
    ):
        self.events = events
        self.redis_consumer = redis_consumer
        self.predict_timeout = predict_timeout
        self.report_setup_run_url = report_setup_run_url

        self._tracker = None
        self._should_exit = False

        self.cog_client = _make_local_http_client()
        self.cog_http_base = "http://localhost:5000"

        app = create_app(events=self.events)
        config = uvicorn.Config(app, port=4900, log_config=None)
        self.server = Server(config)

    def start(self) -> None:
        signal.signal(signal.SIGINT, self.handle_exit)
        signal.signal(signal.SIGTERM, self.handle_exit)

        # Signal pod readiness (when in k8s)
        probes = ProbeHelper()
        probes.ready()

        self.server.start()

        mark = time.perf_counter()
        setup_poll_count = 0

        # First, we wait for the model container to report a successful
        # setup...
        while not self._should_exit:
            try:
                resp = requests.get(
                    self.cog_http_base + "/health-check",
                    timeout=1,
                )
            except requests.exceptions.RequestException:
                pass
            else:
                if resp.status_code == 200:
                    body = resp.json()

                    if body["setup"] is not None:
                        wait_seconds = time.perf_counter() - mark
                        setup_status = body["setup"]["status"]
                        log.info(
                            "model container finished setup",
                            wait_seconds=wait_seconds,
                            status=setup_status,
                        )

                        self._report_setup_run(body["setup"])

                        # if setup failed, exit immediately
                        if (
                            body["status"] != "healthy"
                            or setup_status != schema.Status.SUCCEEDED
                        ):
                            self._abort("model container failed setup")

                        break

            setup_poll_count += 1

            # Print a liveness message every five seconds
            if setup_poll_count % int(5 / SETUP_POLL_INTERVAL) == 0:
                wait_seconds = time.perf_counter() - mark
                log.info(
                    "waiting for model container to complete setup",
                    wait_seconds=wait_seconds,
                )

            time.sleep(SETUP_POLL_INTERVAL)

        # Now, we enter the main loop, pulling prediction requests from Redis
        # and managing the model container.
        while not self._should_exit:
            try:
                message_id, message_json = self.redis_consumer.get()
            except EmptyRedisStream:
                time.sleep(POLL_INTERVAL)  # give the CPU a moment to breathe
                continue

            try:
                self.handle_message(message_id, message_json)
            except Exception:
                log.error("failed to handle message", exc_info=True)
            finally:
                self._tracker = None

        log.info("shutting down worker: bye bye!")

        # TODO: wait for prediction to finish and stuff
        self.server.stop()
        self.server.join()

    def handle_message(self, message_id, message_json) -> None:
        message = json.loads(message_json)

        log.info(
            "received message",
            message_id=message_id,
            queue=self.redis_consumer.redis_input_queue,
            prediction_id=message["id"],
            model_version=message["version"],
        )
        should_cancel = self.redis_consumer.checker(message.get("cancel_key"))
        prediction_id = message["id"]

        self._tracker = PredictionTracker(
            response=schema.PredictionResponse(**message),
            webhook_caller=webhook_caller(message["webhook"]),
        )

        # Override webhook to call us
        message["webhook"] = "http://localhost:4900/webhook"

        # Call the untrusted container to start the prediction
        resp = self.cog_client.post(
            self.cog_http_base + "/predictions",
            json=message,
            headers={"Prefer": "respond-async"},
            timeout=2,
        )
        # FIXME: we should handle schema validation errors here and send
        # appropriate webhooks back up the stack.
        resp.raise_for_status()

        # Wait for any of: completion, shutdown signal. Also check to see if we
        # should cancel the running prediction, and make the appropriate HTTP
        # call if so.
        started_at = datetime.datetime.now()

        while True:
            try:
                event = self.events.get(timeout=POLL_INTERVAL)
            except queue.Empty:
                pass
            else:
                if isinstance(event, Webhook):
                    self._tracker.update_from_webhook_payload(event.payload)
                else:
                    log.warn("received unknown event", event=event)

            if self._tracker.is_complete():
                break

            if should_cancel():
                self._cancel_prediction(prediction_id)
                break

            if self.predict_timeout:
                runtime = (datetime.datetime.now() - started_at).total_seconds()
                if runtime > self.predict_timeout:
                    # Mark the prediction as timed out so we handle the
                    # cancelation webhook appropriately.
                    self._tracker.timed_out()
                    self._cancel_prediction(prediction_id)
                    break

        # Wait for cancelation if necessary
        if not self._tracker.is_complete():
            try:
                event = self.events.get(timeout=CANCEL_WAIT)
            except queue.Empty:
                pass
            else:
                if isinstance(event, Webhook):
                    self._tracker.update_from_webhook_payload(event.payload)
                else:
                    log.warn("received unknown event", event=event)

        self.redis_consumer.ack(message_id)

        if not self._tracker.is_complete():
            # TODO: send our own webhook when this happens
            log.warn(
                "prediction failed to complete after cancelation",
                prediction_id=prediction_id,
            )
            self._abort("prediction failed to complete after cancelation")

    def handle_exit(self, signum: Any, frame: Any) -> None:
        log.warn("received termination signal", signal=signal.Signals(signum).name)
        self._should_exit = True

    def _cancel_prediction(self, prediction_id):
        resp = self.cog_client.post(
            self.cog_http_base + "/predictions/" + prediction_id + "/cancel",
            timeout=1,
        )
        resp.raise_for_status()

    def _report_setup_run(self, payload):
        if not self.report_setup_run_url:
            return

        session = requests_session()
        try:
            # TODO this should be async so we can get on with predictions ASAP
            resp = session.post(self.report_setup_run_url, json=payload)
            resp.raise_for_status()
        except requests.exceptions.RequestException:
            log.warn("failed to report setup run", exc_info=True)

    def _abort(self, message=None):
        resp = self.cog_client.post(
            self.cog_http_base + "/shutdown",
            timeout=1,
        )
        self._should_exit = True
        raise Abort(message)


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
