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
from argparse import ArgumentParser
from mimetypes import guess_type
from pathlib import Path
from typing import Any, Callable, Dict, Iterable, List, Optional, Tuple
from urllib.parse import urlparse

import redis
import requests
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace import Status as TraceStatus
from opentelemetry.trace import StatusCode
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator
from pydantic import ValidationError

from ..files import guess_filename
from ..json import upload_files
from ..predictor import (
    get_input_type,
    get_predictor_ref,
    load_config,
    load_predictor_from_ref,
)
from ..response import Event, Status
from .eventtypes import Done, Heartbeat, Log, PredictionOutput, PredictionOutputType
from .probes import ProbeHelper
from .webhook import requests_session, webhook_caller
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
        report_setup_run_url: Optional[str] = None,
        max_failure_count: Optional[int] = None,
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
        self.report_setup_run_url = report_setup_run_url
        self.max_failure_count = max_failure_count
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
        self.probes = ProbeHelper()

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
            started_at = datetime.datetime.now()

            setup_logs = ""
            try:
                for event in self.worker.setup():
                    if isinstance(event, Log):
                        setup_logs += event.message
                    elif isinstance(event, Done):
                        setup_status = (
                            Status.FAILED if event.error else Status.SUCCEEDED
                        )
            except Exception:
                setup_status = Status.FAILED

            if setup_status == Status.FAILED:
                sys.stderr.write("Setup failed, exiting immediately")
                self.should_exit = True

            completed_at = datetime.datetime.now()

            # Signal pod readiness (when in k8s)
            self.probes.ready()

            if self.report_setup_run_url:
                # TODO this should be async so we can get on with predictions ASAP
                requests_session().post(
                    self.report_setup_run_url,
                    json={
                        "status": setup_status,
                        "started_at": format_datetime(started_at),
                        "completed_at": format_datetime(completed_at),
                        "logs": setup_logs,
                    },
                )

            # TODO deprecate this
            setup_time = (completed_at - started_at).total_seconds()
            self.redis.xadd(
                self.setup_time_queue,
                fields={"duration": setup_time},
                maxlen=self.stats_queue_length,
            )
            sys.stderr.write(f"Setup time: {setup_time:.2f}\n")

        failure_count = 0

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
                        send_response = webhook_caller(webhook)
                    else:
                        redis_key = message["response_queue"]
                        send_response = self.redis_setter(redis_key)

                    sys.stderr.write(
                        f"Received message {message_id} on {self.input_queue}\n"
                    )

                    should_cancel = self.cancelation_checker(message.get("cancel_key"))

                    if "webhook_events_filter" in message:
                        # this will raise an exception if the list isn't valid
                        events_filter = Event.validate(message["webhook_events_filter"])
                    else:
                        events_filter = Event.default_events()

                    for response_event, response in self.run_prediction(
                        message, should_cancel
                    ):
                        if response_event in events_filter:
                            send_response(response)

                    if self.max_failure_count is not None:
                        # Keep track of runs of failures to catch the situation
                        # where the worker has gotten into a bad state where it can
                        # only fail predictions, but isn't exiting.
                        if response["status"] == Status.FAILED:
                            failure_count += 1
                            if failure_count > self.max_failure_count:
                                self.should_exit = True
                                print(
                                    f"Had {failure_count} failures in a row, exiting...",
                                    file=sys.stderr,
                                )
                        else:
                            failure_count = 0

                    self.redis.xack(self.input_queue, self.input_queue, message_id)
                    self.redis.xdel(self.input_queue, message_id)

            except Exception as e:
                tb = traceback.format_exc()
                sys.stderr.write(f"Failed to handle message: {tb}\n")

        sys.stderr.write("Shutting down worker: bye bye!\n")
        self.worker.shutdown()

    def run_prediction(
        self, message: Dict[str, Any], should_cancel: Callable
    ) -> Iterable[Tuple[Event, Dict[str, Any]]]:
        # use the request message as the basis of our response so
        # that we echo back any additional fields sent to us
        response = message
        response["status"] = Status.PROCESSING
        response["output"] = None
        response["logs"] = ""

        started_at = datetime.datetime.now()

        try:
            input_obj = self.InputType(**response["input"])
        except Exception as e:
            response["status"] = Status.FAILED
            response["error"] = str(e)
            yield (Event.COMPLETED, response)

            try:
                input_obj.cleanup()
            except Exception as e:
                sys.stderr.write(f"Cleanup function caught error: {e}")

            return

        response["started_at"] = format_datetime(started_at)
        response["logs"] = ""

        yield (Event.START, response)

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
                    yield (Event.LOGS, response)
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
                        yield (Event.OUTPUT, response)
                    else:
                        assert (
                            response["output"] is None
                        ), "Predictor unexpectedly returned multiple outputs"
                        response["output"] = output

                elif isinstance(event, Done):
                    assert not done_event, "Predictor unexpectedly returned done twice"
                    done_event = event
                else:
                    sys.stderr.write(f"Received unexpected event from worker: {event}")

            completed_at = datetime.datetime.now()
            response["completed_at"] = format_datetime(completed_at)

            # It should only be possible to get here if we got a done event.
            assert done_event

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
            yield (Event.COMPLETED, response)

            try:
                input_obj.cleanup()
            except Exception as e:
                print(f"Cleanup function caught error: {e}", file=sys.stderr)

    def download(self, url: str) -> bytes:
        resp = requests.get(url)
        resp.raise_for_status()
        return resp.content

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
        trace.get_tracer_provider().add_span_processor(span_processor)  # type: ignore

    config = load_config()
    predictor_ref = get_predictor_ref(config)

    parser = ArgumentParser()

    # accept positional arguments for backwards compatibility
    # TODO remove this in a future version of Cog
    parser.add_argument("positional_args", nargs="*")

    parser.add_argument("--redis-host")
    parser.add_argument("--redis-port", type=int)
    parser.add_argument("--input-queue")
    parser.add_argument("--upload-url")
    parser.add_argument("--consumer-id")
    parser.add_argument("--model-id")
    parser.add_argument("--predict-timeout", type=int)
    parser.add_argument("--report-setup-run-url")
    parser.add_argument(
        "--max-failure-count",
        type=int,
        help="Maximum number of consecutive failures before the worker should exit",
    )

    args = parser.parse_args()

    if len(args.positional_args) > 0:
        sys.stderr.write(
            "Positional arguments for queue worker are deprecated. Switch to flag arguments."
        )
        worker = _queue_worker_from_argv(predictor_ref, *args.positional_args)
    else:
        worker = RedisQueueWorker(
            predictor_ref=predictor_ref,
            redis_host=args.redis_host,
            redis_port=args.redis_port,
            input_queue=args.input_queue,
            upload_url=args.upload_url,
            consumer_id=args.consumer_id,
            model_id=args.model_id,
            predict_timeout=args.predict_timeout,
            report_setup_run_url=args.report_setup_run_url,
            max_failure_count=args.max_failure_count,
        )

    worker.start()
