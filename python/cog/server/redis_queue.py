import contextlib
import datetime
import io
import json
import os
import signal
import sys
import time
import traceback
import types
from mimetypes import guess_type
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Tuple
from urllib.parse import urlparse

import redis
import requests
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
    OTLPSpanExporter,
)  # type: ignore
from opentelemetry.sdk.trace import TracerProvider  # type: ignore
from opentelemetry.sdk.trace.export import BatchSpanProcessor  # type: ignore
from opentelemetry.trace import Status as TraceStatus
from opentelemetry.trace import StatusCode
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator
from pydantic import ValidationError
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry

from ..files import guess_filename
from ..json import upload_files
from ..predictor import (
    get_input_type,
    get_predictor_ref,
    load_config,
    load_predictor_from_ref,
)
from ..response import Status
from .eventtypes import Done, Heartbeat, Log, PredictionOutput, PredictionOutputType
from .response_throttler import ResponseThrottler
from .worker import Worker


class RedisQueueWorker:
    SETUP_TIME_QUEUE_SUFFIX = "-setup-time"
    RUN_TIME_QUEUE_SUFFIX = "-run-time"
    STAGE_SETUP = "setup"
    STAGE_RUN = "run"

    def __init__(
        self,
        predictor_ref: str,
        redis_host: str,
        redis_port: int,
        input_queue: str,
        upload_url: str,
        consumer_id: str,
        model_id: Optional[str] = None,
        log_queue: Optional[str] = None,
        predict_timeout: Optional[int] = None,
        redis_db: int = 0,
    ):
        self.worker = Worker(predictor_ref)
        self.redis_host = redis_host
        self.redis_port = redis_port
        self.input_queue = input_queue
        self.upload_url = upload_url
        self.consumer_id = consumer_id
        self.model_id = model_id
        self.log_queue = log_queue
        self.predict_timeout = predict_timeout
        self.redis_db = redis_db
        if self.predict_timeout is not None:
            # 30s grace period allows final responses to be sent and job to be acked
            self.autoclaim_messages_after = self.predict_timeout + 30
        else:
            # retry after 10 minutes by default
            self.autoclaim_messages_after = 10 * 60

        # Set up types
        predictor = load_predictor_from_ref(predictor_ref)
        self.InputType = get_input_type(predictor)

        self.redis = redis.Redis(
            host=self.redis_host, port=self.redis_port, db=self.redis_db
        )
        self.should_exit = False
        self.setup_time_queue = input_queue + self.SETUP_TIME_QUEUE_SUFFIX
        self.predict_time_queue = input_queue + self.RUN_TIME_QUEUE_SUFFIX
        self.stats_queue_length = 100
        self.tracer = trace.get_tracer("cog")

        sys.stderr.write(
            f"Connected to Redis: {self.redis_host}:{self.redis_port} (db {self.redis_db})\n"
        )

    def signal_exit(self, signum: Any, frame: Any) -> None:
        self.should_exit = True
        sys.stderr.write("Caught SIGTERM, exiting...\n")

    def receive_message(self) -> Tuple[Optional[str], Optional[str]]:
        # first, try to autoclaim old messages from pending queue
        raw_messages = self.redis.execute_command(
            "XAUTOCLAIM",
            self.input_queue,
            self.input_queue,
            self.consumer_id,
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
            return key.decode(), raw_message[1].decode()

        # if no old messages exist, get message from main queue
        raw_messages = self.redis.xreadgroup(
            groupname=self.input_queue,
            consumername=self.consumer_id,
            streams={self.input_queue: ">"},
            count=1,
            block=1000,
        )
        if not raw_messages:
            return None, None

        # format: [[b'mystream', [(b'1619395583065-0', {b'mykey': b'myval6'})]]]
        key, raw_message = raw_messages[0][1][0]
        return key.decode(), raw_message[b"value"].decode()

    def start(self) -> None:
        with self.tracer.start_as_current_span(name="redis_queue.setup") as span:
            signal.signal(signal.SIGTERM, self.signal_exit)
            start_time = time.time()

            # TODO(bfirsh): setup should time out too, but we don't display these logs to the user, so don't timeout to avoid confusion
            for event in self.worker.setup():
                # TODO: send worker logs somewhere useful!
                pass

            setup_time = time.time() - start_time
            self.redis.xadd(
                self.setup_time_queue,
                fields={"duration": setup_time},
                maxlen=self.stats_queue_length,
            )
            sys.stderr.write(f"Setup time: {setup_time:.2f}\n")

        sys.stderr.write(f"Waiting for message on {self.input_queue}\n")
        while not self.should_exit:
            try:
                message_id, message_json = self.receive_message()
                if message_json is None:
                    # tight loop in order to respect self.should_exit
                    continue

                time_in_queue = calculate_time_in_queue(message_id)  # type: ignore
                message = json.loads(message_json)

                # Check whether the incoming message includes details of an
                # OpenTelemetry trace, to make distributed tracing work. The
                # value should look like:
                #
                #     00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
                if "traceparent" in message:
                    context = TraceContextTextMapPropagator().extract(
                        {"traceparent": message["traceparent"]}
                    )
                else:
                    context = None

                with self.tracer.start_as_current_span(
                    name="redis_queue.process_message",
                    context=context,
                    attributes={"time_in_queue": time_in_queue},
                ) as span:
                    webhook = message.get("webhook")
                    if webhook is not None:
                        send_response = self.webhook_caller(webhook)
                    else:
                        redis_key = message["response_queue"]
                        send_response = self.redis_setter(redis_key)

                    sys.stderr.write(
                        f"Received message {message_id} on {self.input_queue}\n"
                    )

                    should_cancel = self.cancelation_checker(message.get("cancel_key"))

                    # use the request message as the basis of our response so
                    # that we echo back any additional fields sent to us
                    response = message
                    response["status"] = Status.PROCESSING
                    response["output"] = None
                    response["logs"] = ""

                    cleanup_functions: List[Callable] = []
                    try:
                        start_time = time.time()
                        self.handle_message(
                            send_response, response, cleanup_functions, should_cancel
                        )
                        self.redis.xack(self.input_queue, self.input_queue, message_id)
                        self.redis.xdel(
                            self.input_queue, message_id
                        )  # xdel to be able to get stream size
                        run_time = time.time() - start_time
                        self.redis.xadd(
                            self.predict_time_queue,
                            fields={"duration": run_time},
                            maxlen=self.stats_queue_length,
                        )
                        sys.stderr.write(f"Run time for {message_id}: {run_time:.2f}\n")
                    except Exception as e:
                        response["status"] = Status.FAILED
                        response["error"] = str(e)
                        response["completed_at"] = format_datetime(
                            datetime.datetime.now()
                        )
                        send_response(response)
                        self.redis.xack(self.input_queue, self.input_queue, message_id)
                        self.redis.xdel(self.input_queue, message_id)
                    finally:
                        for cleanup_function in cleanup_functions:
                            try:
                                cleanup_function()
                            except Exception as e:
                                sys.stderr.write(f"Cleanup function caught error: {e}")
            except Exception as e:
                tb = traceback.format_exc()
                sys.stderr.write(f"Failed to handle message: {tb}\n")

        sys.stderr.write("Shutting down worker: bye bye!\n")
        self.worker.shutdown()

    def handle_message(
        self,
        send_response: Callable,
        response: Dict[str, Any],
        cleanup_functions: List[Callable],
        should_cancel: Callable,
    ) -> None:
        span = trace.get_current_span()
        started_at = datetime.datetime.now()

        try:
            input_obj = self.InputType(**response["input"])
        except ValidationError as e:
            tb = traceback.format_exc()
            sys.stderr.write(tb)
            response["status"] = Status.FAILED
            response["error"] = str(e)
            send_response(response)
            span.record_exception(e)
            span.set_status(TraceStatus(status_code=StatusCode.ERROR))
            return
        finally:
            cleanup_functions.append(input_obj.cleanup)

        response["started_at"] = format_datetime(started_at)
        response["logs"] = ""

        send_response(response)

        timed_out = False
        was_canceled = False
        done_event = None
        output_type = None

        try:
            for event in self.worker.predict(payload=input_obj.dict(), poll=0.1):
                if not was_canceled and should_cancel():
                    was_canceled = True
                    self.worker.cancel()

                if not timed_out and self.predict_timeout:
                    runtime = (datetime.datetime.now() - started_at).total_seconds()
                    if runtime > self.predict_timeout:
                        timed_out = True
                        self.worker.cancel()

                if isinstance(event, Heartbeat):
                    # Heartbeat events exist solely to ensure that we have a
                    # regular opportunity to check for cancelation and
                    # timeouts.
                    #
                    # We don't need to do anything with them.
                    pass
                elif isinstance(event, Log):
                    response["logs"] += event.message
                    send_response(response)
                elif isinstance(event, PredictionOutputType):
                    # Note: this error message will be seen by users so it is
                    # intentionally vague about what has gone wrong.
                    assert output_type is None, "Predictor returned unexpected output"
                    output_type = event
                    if output_type.multi:
                        response["output"] = []
                elif isinstance(event, PredictionOutput):
                    # Note: this error message will be seen by users so it is
                    # intentionally vague about what has gone wrong.
                    assert (
                        output_type is not None
                    ), "Predictor returned unexpected output"

                    output = self.upload_files(event.payload)

                    if output_type.multi:
                        response["output"].append(output)
                        send_response(response)
                    else:
                        assert (
                            response["output"] is None
                        ), "Predictor unexpectedly returned multiple outputs"
                        response["output"] = output

                elif isinstance(event, Done):
                    done_event = event
                else:
                    sys.stderr.write(f"Received unexpected event from worker: {event}")

            completed_at = datetime.datetime.now()
            response["completed_at"] = format_datetime(completed_at)

            if done_event.canceled and was_canceled:
                response["status"] = Status.CANCELED
            elif done_event.canceled and timed_out:
                response["status"] = Status.FAILED
                response["error"] = "Prediction timed out"
            elif done_event.error:
                response["status"] = Status.FAILED
                response["error"] = str(done_event.error_detail)
            else:
                response["status"] = Status.SUCCEEDED
                response["metrics"] = {
                    "predict_time": (completed_at - started_at).total_seconds()
                }
        except Exception as e:
            self.should_exit = True
            completed_at = datetime.datetime.now()
            response["completed_at"] = format_datetime(completed_at)
            response["status"] = Status.FAILED
            response["error"] = str(e)
        finally:
            send_response(response)

    def download(self, url: str) -> bytes:
        resp = requests.get(url)
        resp.raise_for_status()
        return resp.content

    def webhook_caller(self, webhook: str) -> Callable:
        response_interval = float(os.environ.get("COG_THROTTLE_RESPONSE_INTERVAL", 0))
        throttler = ResponseThrottler(response_interval=response_interval)

        # This session will retry requests up to 12 times, with exponential
        # backoff. In total it'll try for up to roughly 320 seconds, providing
        # resilience through temporary networking and availability issues.
        session = requests.Session()
        adapter = HTTPAdapter(
            max_retries=Retry(
                total=12,
                backoff_factor=0.1,
                status_forcelist=[429, 500, 502, 503, 504],
                allowed_methods=["POST"],
            )
        )
        session.mount("http://", adapter)
        session.mount("https://", adapter)

        def caller(response: Any) -> None:
            if throttler.should_send_response(response):
                if Status.is_terminal(response["status"]):
                    # For terminal updates, retry persistently
                    session.post(webhook, json=response)
                else:
                    # For other requests, don't retry
                    requests.post(webhook, json=response)
                throttler.update_last_sent_response_time()

        return caller

    def redis_setter(self, redis_key: str) -> Callable:
        def setter(response: Any) -> None:
            self.redis.set(redis_key, json.dumps(response))

        return setter

    def cancelation_checker(self, redis_key: str) -> Callable:
        def checker() -> bool:
            return redis_key is not None and self.redis.exists(redis_key) > 0

        return checker

    def upload_files(self, obj: Any) -> Any:
        def upload_file(fh: io.IOBase) -> str:
            filename = guess_filename(fh)
            content_type, _ = guess_type(filename)

            resp = requests.put(
                ensure_trailing_slash(self.upload_url) + filename,
                fh,  # type: ignore
                headers={"Content-type": content_type},
            )
            resp.raise_for_status()

            # strip any signing gubbins from the URL
            final_url = urlparse(resp.url)._replace(query="").geturl()

            return final_url

        return upload_files(obj, upload_file)


def calculate_time_in_queue(message_id: str) -> float:
    """
    Calculate how long a message spent in the queue based on the timestamp in
    the message ID.
    """
    now = time.time()
    queue_time = int(message_id[:13]) / 1000.0
    return now - queue_time


def format_datetime(timestamp: datetime.datetime) -> str:
    """
    Formats a datetime in ISO8601 with a trailing Z, so it's also RFC3339 for
    easier parsing by things like Golang.
    """
    return timestamp.isoformat() + "Z"


def ensure_trailing_slash(url: str) -> str:
    """
    Adds a trailing slash to `url` if not already present, and then returns it.
    """
    if url.endswith("/"):
        return url
    else:
        return url + "/"


def _queue_worker_from_argv(
    predictor_ref: str,
    redis_host: str,
    redis_port: str,
    input_queue: str,
    upload_url: str,
    comsumer_id: str,
    model_id: str,
    log_queue: str,
    predict_timeout: Optional[str] = None,
) -> RedisQueueWorker:
    """
    Construct a RedisQueueWorker object from sys.argv, taking into account optional arguments and types.

    This is intensely fragile. This should be kwargs or JSON or something like that.
    """
    if predict_timeout is not None:
        predict_timeout_int = int(predict_timeout)
    else:
        predict_timeout_int = None
    return RedisQueueWorker(
        predictor_ref,
        redis_host,
        int(redis_port),
        input_queue,
        upload_url,
        comsumer_id,
        model_id,
        log_queue,
        predict_timeout_int,
    )


if __name__ == "__main__":
    # Enable OpenTelemetry if the env vars are present. If this block isn't
    # run, all the opentelemetry calls are no-ops.
    if "OTEL_SERVICE_NAME" in os.environ:
        trace.set_tracer_provider(TracerProvider())
        span_processor = BatchSpanProcessor(OTLPSpanExporter())
        trace.get_tracer_provider().add_span_processor(span_processor)

    config = load_config()
    predictor_ref = get_predictor_ref(config)

    worker = _queue_worker_from_argv(predictor_ref, *sys.argv[1:])
    worker.start()
