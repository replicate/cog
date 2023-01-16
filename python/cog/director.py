import datetime
import logging
import os
import json
import multiprocessing
import signal
import sys
import threading
import time
import traceback
from argparse import ArgumentParser
from typing import Any, Callable, Dict, Optional, Tuple

from fastapi import FastAPI
from fastapi.responses import JSONResponse
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry
import redis
import requests
import structlog
import uvicorn

from . import schema
from .json import make_encodeable
from .logging import setup_logging
from .server.probes import ProbeHelper
from .server.webhook import webhook_caller

log = structlog.get_logger("cog.director")

# How often to check for model container setup on boot.
SETUP_POLL_INTERVAL = 0.1

# How often to check for cancelation or shutdown signals while a prediction is
# running, in seconds. 100ms mirrors the value currently supplied to the `poll`
# keyword argument for Worker.predict(...) in the redis queue worker code.
POLL_INTERVAL = 0.1


class EmptyRedisStream(Exception):
    pass


class RedisConsumer:
    def __init__(
        self,
        redis_url: str,
        redis_input_queue: str,
        redis_consumer_id: str,
        predict_timeout: Optional[int] = None,
    ):
        self.redis_url = redis_url
        self.redis_input_queue = redis_input_queue
        self.redis_consumer_id = redis_consumer_id

        if predict_timeout is not None:
            # 30s grace period allows final responses to be sent and job to be acked
            self.autoclaim_messages_after = predict_timeout + 30
        else:
            # retry after 10 minutes by default
            self.autoclaim_messages_after = 10 * 60

        self.redis = redis.from_url(self.redis_url)
        log.info("connected to redis", url=self.redis_url)

    def get(self) -> Tuple[str, str]:
        log.debug("waiting for message", queue=self.redis_input_queue)

        # first, try to autoclaim old messages from pending queue
        raw_messages = self.redis.execute_command(
            "XAUTOCLAIM",
            self.redis_input_queue,
            self.redis_input_queue,
            self.redis_consumer_id,
            str(self.autoclaim_messages_after * 1000),
            "0-0",
            "COUNT",
            1,
        )
        # format: [[b'1619393873567-0', [b'mykey', b'myval']]]
        # since redis==4.3.4 an empty response from xautoclaim is indicated by [[b'0-0', []]]
        if raw_messages and raw_messages[0] is not None and len(raw_messages[0]) == 2:
            key, raw_message = raw_messages[0]
            assert raw_message[0] == b"value"

            message_id = key.decode()
            message = raw_message[1].decode()
            log.info(
                "received message", message_id=message_id, queue=self.redis_input_queue
            )
            return message_id, message

        # if no old messages exist, get message from main queue
        raw_messages = self.redis.xreadgroup(
            groupname=self.redis_input_queue,
            consumername=self.redis_consumer_id,
            streams={self.redis_input_queue: ">"},
            count=1,
            block=1000,
        )
        if not raw_messages:
            raise EmptyRedisStream()

        # format: [[b'mystream', [(b'1619395583065-0', {b'mykey': b'myval6'})]]]
        key, raw_message = raw_messages[0][1][0]
        message_id = key.decode()
        message = raw_message[b"value"].decode()
        log.info(
            "received message", message_id=message_id, queue=self.redis_input_queue
        )
        return message_id, message

    def ack(self, message_id: str) -> None:
        self.redis.xack(self.redis_input_queue, self.redis_input_queue, message_id)
        self.redis.xdel(self.redis_input_queue, message_id)
        log.info("acked message", message_id=message_id, queue=self.redis_input_queue)

    def checker(self, redis_key: str) -> Callable:
        def checker_() -> bool:
            return redis_key is not None and self.redis.exists(redis_key) > 0

        return checker_


class DirectorQueueWorker:
    def __init__(
        self,
        redis_consumer: RedisConsumer,
        predict_timeout: int,
        prediction_event: threading.Event,
        shutdown_event: threading.Event,
        prediction_request_pipe: multiprocessing.connection.Connection,
    ):
        self.redis_consumer = redis_consumer

        self.prediction_event = prediction_event
        self.shutdown_event = shutdown_event
        self.prediction_request_pipe = prediction_request_pipe

        self.predict_timeout = predict_timeout

        self.cog_client = _make_local_http_client()
        self.cog_http_base = "http://localhost:5000"

    def start(self) -> None:
        mark = time.perf_counter()
        setup_poll_count = 0

        # First, we wait for the model container to report a successful
        # setup...
        while not self.shutdown_event.is_set():
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

                    if (
                        body["status"] == "healthy"
                        and body["setup"] is not None
                        and body["setup"]["status"] == schema.Status.SUCCEEDED
                    ):
                        wait_seconds = time.perf_counter() - mark
                        log.info(
                            "model container completed setup", wait_seconds=wait_seconds
                        )

                        # TODO: send setup-run webhook
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
        while not self.shutdown_event.is_set():
            try:
                self.handle_message()
            except Exception:
                log.exception("failed to handle message")

        log.info("shutting down worker: bye bye!")

    def handle_message(self) -> None:
        try:
            message_id, message_json = self.redis_consumer.get()
        except EmptyRedisStream:
            time.sleep(POLL_INTERVAL)  # give the CPU a moment to breathe
            return

        message = json.loads(message_json)
        should_cancel = self.redis_consumer.checker(message.get("cancel_key"))
        prediction_id = message["id"]

        # Send the original request to the webserver, so it can trust the fields
        while self.prediction_request_pipe.poll():
            # clear the pipe first, out of an abundance of caution
            self.prediction_request_pipe.recv()
        self.prediction_request_pipe.send(message)

        # Reset the prediction event to indicate that a prediction is running
        self.prediction_event.clear()

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
        while True:
            if self.prediction_event.wait(POLL_INTERVAL):
                break

            if should_cancel():
                resp = self.cog_client.post(
                    self.cog_http_base + "/predictions/" + prediction_id + "/cancel",
                    timeout=1,
                )
                resp.raise_for_status()

            if self.shutdown_event.is_set():
                return

        self.redis_consumer.ack(message_id)


def run_queue_worker(**kwargs: Any) -> None:
    worker = DirectorQueueWorker(**kwargs)
    worker.start()


def create_app(
    redis_consumer: RedisConsumer, predict_timeout: int, max_failure_count: int
) -> FastAPI:
    app = FastAPI(title="Director")

    # Used to signal between webserver and queue worker when a prediction is
    # running or not.
    app.state.prediction_event = threading.Event()

    # Used to signal when the queue worker should shut down.
    app.state.shutdown_event = threading.Event()

    # Used to send the original prediction request from the queue worker to the
    # webserver for constructing outgoing webhooks.
    (
        app.state.prediction_request_pipe,
        worker_prediction_request_pipe,
    ) = multiprocessing.Pipe()
    app.state.prediction_request = None

    # Number of consecutive failures seen
    app.state.failure_count = 0

    worker = threading.Thread(
        target=run_queue_worker,
        kwargs=dict(
            redis_consumer=redis_consumer,
            predict_timeout=predict_timeout,
            prediction_event=app.state.prediction_event,
            shutdown_event=app.state.shutdown_event,
            prediction_request_pipe=worker_prediction_request_pipe,
        ),
    )

    def check_failure_count() -> None:
        if max_failure_count is None:
            return
        if app.state.failure_count <= max_failure_count:
            return

        log.error(
            "saw too many failures in a row, exiting...",
            failure_count=app.state.failure_count,
        )

        # FIXME: find a better way to shut down uvicorn
        os.kill(os.getpid(), signal.SIGTERM)

    @app.on_event("startup")
    def startup() -> None:
        # Signal pod readiness (when in k8s)
        probes = ProbeHelper()
        probes.ready()

        worker.start()

    @app.on_event("shutdown")
    def shutdown() -> None:
        app.state.shutdown_event.set()

    @app.post("/webhook")
    def webhook(payload: schema.PredictionResponse) -> Any:
        # TODO the logic here seems weird, might need to invert the variable
        # name to something like `prediction_running`?
        if app.state.prediction_event.is_set():
            return JSONResponse(
                {"detail": "cannot receive webhooks when no prediction is running"},
                status_code=409,
            )

        if payload.status is None:
            return JSONResponse(
                {"detail": "webhook payload must have a status"}, status_code=400
            )

        if app.state.prediction_request is None:
            log.info("Getting updated prediction_request from pipe")
            # TODO how defensive do we need to be reading from this pipe?
            app.state.prediction_request = app.state.prediction_request_pipe.recv()
            app.state.send_webhook = webhook_caller(
                app.state.prediction_request["webhook"]
            )

        # only permit a limited set of keys from the payload, to prevent
        # untrusted code from setting things like IDs and internal data
        outgoing_response = make_encodeable(
            {
                **app.state.prediction_request,
                **allowed_fields(payload.dict()),
            }
        )
        log.info("Sending outgoing webhook", payload=outgoing_response)
        app.state.send_webhook(outgoing_response)

        if schema.Status.is_terminal(payload.status):
            app.state.prediction_request = None
            app.state.prediction_event.set()

        if payload.status == schema.Status.FAILED:
            app.state.failure_count += 1
            check_failure_count()
        else:
            app.state.failure_count = 0

        return JSONResponse({"status": "ok"}, status_code=200)

    return app


ALLOWED_FIELDS_FROM_UNTRUSTED_CONTAINER = (
    # FIXME: we shouldn't trust the timings (or derived metrics) either
    "completed_at",
    "started_at",
    "metrics",
    # Prediction output and output metadata
    "error",
    "logs",
    "output",
    "status",
)


def allowed_fields(payload: dict):
    return {
        k: v for k, v in payload.items() if k in ALLOWED_FIELDS_FROM_UNTRUSTED_CONTAINER
    }


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


def _die(signum: Any, frame: Any) -> None:
    log.warning("caught early SIGTERM: exiting immediately!")
    sys.exit(1)


if __name__ == "__main__":
    # We are probably running as PID 1 so need to explicitly register a handler
    # to die on SIGTERM. This will be overwritten once we start uvicorn.
    signal.signal(signal.SIGTERM, _die)

    parser = ArgumentParser()

    parser.add_argument("--redis-url", required=True)
    parser.add_argument("--redis-input-queue", required=True)
    parser.add_argument("--redis-consumer-id", required=True)
    parser.add_argument("--predict-timeout", type=int, default=1800)
    parser.add_argument(
        "--max-failure-count",
        type=int,
        default=5,
        help="Maximum number of consecutive failures before the worker should exit",
    )

    setup_logging(log_level=logging.INFO)

    args = parser.parse_args()

    redis_consumer = RedisConsumer(
        redis_url=args.redis_url,
        redis_input_queue=args.redis_input_queue,
        redis_consumer_id=args.redis_consumer_id,
        predict_timeout=args.predict_timeout,
    )
    app = create_app(
        redis_consumer=redis_consumer,
        predict_timeout=args.predict_timeout,
        max_failure_count=args.max_failure_count,
    )
    uvicorn.run(app, port=4900, log_config=None)
