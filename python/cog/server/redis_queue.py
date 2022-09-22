import datetime
import io
import json
import os
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Tuple
import signal
import sys
import traceback
import time
import types
import contextlib

from pydantic import ValidationError
import redis
import requests

from ..predictor import BasePredictor, get_input_type, load_predictor, load_config
from ..json import upload_files
from ..response import Status
from .runner import PredictionRunner

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter  # type: ignore
from opentelemetry.sdk.trace import TracerProvider  # type: ignore
from opentelemetry.sdk.trace.export import BatchSpanProcessor  # type: ignore
from opentelemetry.trace import Status as TraceStatus, StatusCode
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator


class RedisQueueWorker:
    SETUP_TIME_QUEUE_SUFFIX = "-setup-time"
    RUN_TIME_QUEUE_SUFFIX = "-run-time"
    STAGE_SETUP = "setup"
    STAGE_RUN = "run"

    def __init__(
        self,
        predictor: BasePredictor,
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
        self.runner = PredictionRunner(predict_timeout=predict_timeout)
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
            self.runner.setup()

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

                    # use the request message as the basis of our response so
                    # that we echo back any additional fields sent to us
                    response = message
                    response["status"] = Status.PROCESSING
                    response["output"] = None
                    response["logs"] = []

                    cleanup_functions: List[Callable] = []
                    try:
                        start_time = time.time()
                        self.handle_message(send_response, response, cleanup_functions)
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
                        response["completed_at"] = (
                            datetime.datetime.now().isoformat() + "Z"
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

        sys.stderr.write("Closing runner, bye bye!\n")
        self.runner.close()

    def handle_message(
        self,
        send_response: Callable,
        response: Dict[str, Any],
        cleanup_functions: List[Callable],
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

        cleanup_functions.append(input_obj.cleanup)

        self.runner.run(**input_obj.dict())

        response["started_at"] = started_at.isoformat() + "Z"

        logs: List[str] = []
        response["logs"] = logs

        send_response(response)

        # just send logs until output starts
        while self.runner.is_processing() and not self.runner.has_output_waiting():
            if self.runner.has_logs_waiting():
                logs.extend(self.runner.read_logs())
                send_response(response)

        if self.runner.error() is not None:
            response["status"] = Status.FAILED
            response["error"] = str(self.runner.error())  # type: ignore
            response["completed_at"] = datetime.datetime.now().isoformat() + "Z"
            send_response(response)
            span.record_exception(self.runner.error())
            span.set_status(TraceStatus(status_code=StatusCode.ERROR))
            return

        span.add_event("received first output")

        if self.runner.is_output_generator():
            output = response["output"] = []

            while self.runner.is_processing():
                # TODO: restructure this to avoid the tight CPU-eating loop
                if self.runner.has_output_waiting() or self.runner.has_logs_waiting():
                    # Object has already passed through `make_encodeable()` in the Runner, so all we need to do here is upload the files
                    new_output = [
                        self.upload_files(o) for o in self.runner.read_output()
                    ]
                    new_logs = self.runner.read_logs()

                    # sometimes it'll say there's output when there's none
                    if new_output == [] and new_logs == []:
                        continue

                    output.extend(new_output)
                    logs.extend(new_logs)

                    # we could `time.sleep(0.1)` and check `is_processing()`
                    # here to give the predictor subprocess a chance to exit
                    # so we don't send a double message for final output, at
                    # the cost of extra latency
                    send_response(response)

            if self.runner.error() is not None:
                response["status"] = Status.FAILED
                response["error"] = str(self.runner.error())  # type: ignore
                response["completed_at"] = datetime.datetime.now().isoformat() + "Z"
                send_response(response)
                span.record_exception(self.runner.error())
                span.set_status(TraceStatus(status_code=StatusCode.ERROR))
                return

            span.add_event("received final output")

            response["status"] = Status.SUCCEEDED
            completed_at = datetime.datetime.now()
            response["completed_at"] = completed_at.isoformat() + "Z"
            response["metrics"] = {
                "predict_time": (completed_at - started_at).total_seconds()
            }
            output.extend(self.upload_files(o) for o in self.runner.read_output())
            logs.extend(self.runner.read_logs())
            send_response(response)

        else:
            # just send logs until output ends
            while self.runner.is_processing():
                if self.runner.has_logs_waiting():
                    logs.extend(self.runner.read_logs())
                    send_response(response)

            if self.runner.error() is not None:
                response["status"] = Status.FAILED
                response["error"] = str(self.runner.error())  # type: ignore
                response["completed_at"] = datetime.datetime.now().isoformat() + "Z"
                send_response(response)
                span.record_exception(self.runner.error())
                span.set_status(TraceStatus(status_code=StatusCode.ERROR))
                return

            output = self.runner.read_output()
            assert len(output) == 1

            response["status"] = Status.SUCCEEDED
            completed_at = datetime.datetime.now()
            response["completed_at"] = completed_at.isoformat() + "Z"
            response["metrics"] = {
                "predict_time": (completed_at - started_at).total_seconds()
            }
            response["output"] = self.upload_files(output[0])
            logs.extend(self.runner.read_logs())
            send_response(response)

    def download(self, url: str) -> bytes:
        resp = requests.get(url)
        resp.raise_for_status()
        return resp.content

    def webhook_caller(self, webhook: str) -> Callable:
        def caller(response: Any) -> None:
            requests.post(webhook, json=response)

        return caller

    def redis_setter(self, redis_key: str) -> Callable:
        def setter(response: Any) -> None:
            self.redis.set(redis_key, json.dumps(response))

        return setter

    def upload_files(self, obj: Any) -> Any:
        def upload_file(fh: io.IOBase) -> str:
            resp = requests.put(self.upload_url, files={"file": fh})
            resp.raise_for_status()
            return resp.json()["url"]

        return upload_files(obj, upload_file)


def calculate_time_in_queue(message_id: str) -> float:
    """
    Calculate how long a message spent in the queue based on the timestamp in
    the message ID.
    """
    now = time.time()
    queue_time = int(message_id[:13]) / 1000.0
    return now - queue_time


def _queue_worker_from_argv(
    predictor: BasePredictor,
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
        predictor,
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
    predictor = load_predictor(config)

    worker = _queue_worker_from_argv(predictor, *sys.argv[1:])
    worker.start()
