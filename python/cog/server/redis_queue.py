import inspect
import contextlib
import io
from io import BytesIO
import json
from pathlib import Path
from typing import Optional
import signal
import sys
import traceback
import time
import types

import redis
import requests
from werkzeug.datastructures import FileStorage


from ..input import InputValidationError, validate_and_convert_inputs
from ..json import to_json
from ..predictor import Predictor, run_prediction, load_predictor


class RedisQueueWorker:
    SETUP_TIME_QUEUE_SUFFIX = "-setup-time"
    RUN_TIME_QUEUE_SUFFIX = "-run-time"
    STAGE_SETUP = "setup"
    STAGE_RUN = "run"

    def __init__(
        self,
        predictor: Predictor,
        redis_host: str,
        redis_port: int,
        input_queue: str,
        consumer_id: str,
        upload_url: str,
        model_id: Optional[str] = None,
        log_queue: Optional[str] = None,
        redis_db: int = 0,
    ):
        self.predictor = predictor
        self.model_id = model_id
        self.redis_host = redis_host
        self.redis_port = redis_port
        self.input_queue = input_queue
        self.log_queue = log_queue
        self.consumer_id = consumer_id
        self.upload_url = upload_url
        self.redis_db = redis_db
        # TODO: respect max_processing_time in message handling
        self.max_processing_time = 10 * 60  # timeout after 10 minutes
        self.redis = redis.Redis(
            host=self.redis_host, port=self.redis_port, db=self.redis_db
        )
        self.should_exit = False
        self.setup_time_queue = input_queue + self.SETUP_TIME_QUEUE_SUFFIX
        self.predict_time_queue = input_queue + self.RUN_TIME_QUEUE_SUFFIX
        self.stats_queue_length = 100

        sys.stderr.write(
            f"Connected to Redis: {self.redis_host}:{self.redis_port} (db {self.redis_db})\n"
        )

    def signal_exit(self, signum, frame):
        self.should_exit = True
        sys.stderr.write("Caught SIGTERM, exiting...\n")

    def receive_message(self):
        # first, try to autoclaim old messages from pending queue
        _, raw_messages = self.redis.execute_command(
            "XAUTOCLAIM",
            self.input_queue,
            self.input_queue,
            self.consumer_id,
            str(self.max_processing_time * 1000),
            "0-0",
            "COUNT",
            1,
        )
        # format: [[b'1619393873567-0', [b'mykey', b'myval']]]
        if raw_messages and raw_messages[0] is not None:
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

    def start(self):
        signal.signal(signal.SIGTERM, self.signal_exit)
        start_time = time.time()

        with self.capture_log(self.STAGE_SETUP, self.model_id):
            self.predictor.setup()

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

                message = json.loads(message_json)
                prediction_id = message["id"]
                response_queue = message["response_queue"]
                sys.stderr.write(
                    f"Received message {prediction_id} on {self.input_queue}\n"
                )
                cleanup_functions = []
                try:
                    start_time = time.time()
                    self.handle_message(response_queue, message, cleanup_functions)
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
                    sys.stderr.write(f"Run time: {run_time:.2f}\n")
                except Exception as e:
                    tb = traceback.format_exc()

                    with self.capture_log(self.STAGE_RUN, prediction_id):
                        sys.stderr.write(f"{tb}\n")
                    self.push_error(response_queue, e)
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

    def handle_message(self, response_queue, message, cleanup_functions):
        inputs = {}
        raw_inputs = message["inputs"]
        prediction_id = message["id"]

        for k, v in raw_inputs.items():
            if "value" in v and v["value"] != "":
                inputs[k] = v["value"]
            else:
                file_url = v["file"]["url"]
                sys.stderr.write(f"Downloading file from {file_url}\n")
                value_bytes = self.download(file_url)
                inputs[k] = FileStorage(
                    stream=BytesIO(value_bytes), filename=v["file"]["name"]
                )
        try:
            inputs = validate_and_convert_inputs(
                self.predictor, inputs, cleanup_functions
            )
        except InputValidationError as e:
            tb = traceback.format_exc()
            sys.stderr.write(tb)
            self.push_error(response_queue, e)
            return

        with self.capture_log(self.STAGE_RUN, prediction_id):
            return_value = self.predictor.predict(**inputs)
        if isinstance(return_value, types.GeneratorType):
            last_result = None
            for i, result in enumerate(return_value):
                # push the previous result, so we can eventually detect the last iteration
                if i > 0:
                    self.push_result(response_queue, last_result, status="processing")
                if isinstance(result, Path):
                    cleanup_functions.append(result.unlink)
                last_result = result

            # push the last result
            self.push_result(response_queue, last_result, status="success")
        else:
            if isinstance(return_value, Path):
                cleanup_functions.append(return_value.unlink)
            self.push_result(response_queue, return_value, status="success")

    def download(self, url):
        resp = requests.get(url)
        resp.raise_for_status()
        return resp.content

    def push_error(self, response_queue, error):
        message = json.dumps(
            {
                "error": str(error),
            }
        )
        sys.stderr.write(f"Pushing error to {response_queue}\n")
        self.redis.rpush(response_queue, message)

    def push_result(self, response_queue, result, status):
        if isinstance(result, Path):
            message = {
                "file": {
                    "url": self.upload_to_temp(result),
                    "name": result.name,
                }
            }
        elif isinstance(result, str):
            message = {
                "value": result,
            }
        else:
            message = {
                "value": to_json(result),
            }

        message["status"] = status

        sys.stderr.write(f"Pushing successful result to {response_queue}\n")
        self.redis.rpush(response_queue, json.dumps(message))

    def upload_to_temp(self, path: Path) -> str:
        sys.stderr.write(
            f"Uploading {path.name} to temporary storage at {self.upload_url}\n"
        )
        resp = requests.put(
            self.upload_url, files={"file": (path.name, path.open("rb"))}
        )
        resp.raise_for_status()
        return resp.json()["url"]

    @contextlib.contextmanager
    def capture_log(self, stage, id):
        """
        Send each log line to a redis RPUSH queue in addition to an
        existing output stream.
        """

        class QueueLogger(io.IOBase):
            def __init__(self, redis, queue, old_out):
                super().__init__()
                self.redis = redis
                self.queue = queue
                self.old_out = old_out

            def write(self, buf):
                for line in buf.rstrip().splitlines():
                    self.write_line(line)

            def write_line(self, line):
                self.redis.rpush(self.queue, self.log_message(line))
                self.old_out.write(line + "\n")

            def log_message(self, line):
                timestamp_sec = time.time()
                return json.dumps(
                    {
                        "stage": stage,
                        "id": id,
                        "line": line,
                        "timestamp_sec": timestamp_sec,
                    }
                )

        if self.log_queue is None:
            yield

        else:
            old_stdout = sys.stdout
            old_stderr = sys.stderr
            try:
                # TODO(andreas): differentiate stdout/stderr?
                sys.stdout = QueueLogger(self.redis, self.log_queue, old_stdout)
                sys.stderr = QueueLogger(self.redis, self.log_queue, old_stderr)
                yield
            finally:
                sys.stdout = old_stdout
                sys.stderr = old_stderr


if __name__ == "__main__":
    predictor = load_predictor()
    worker = RedisQueueWorker(
        predictor,
        redis_host=sys.argv[1],
        redis_port=sys.argv[2],
        input_queue=sys.argv[3],
        upload_url=sys.argv[4],
        consumer_id=sys.argv[5],
        model_id=sys.argv[6],
        log_queue=sys.argv[7],
    )
    worker.start()
