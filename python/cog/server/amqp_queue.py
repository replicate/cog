from io import BytesIO
import pika
import json
from pathlib import Path
from typing import Optional
import signal
import sys
import traceback
import time
import types
import contextlib

import redis
import requests
from werkzeug.datastructures import FileStorage

from .redis_log_capture import capture_log
from ..input import InputValidationError, validate_and_convert_inputs
from ..json import to_json
from ..predictor import Predictor, load_predictor

class AMQPQueueWorker:
    SETUP_TIME_QUEUE_SUFFIX = "-setup-time"
    RUN_TIME_QUEUE_SUFFIX = "-run-time"
    STAGE_SETUP = "setup"
    STAGE_RUN = "run"

    def __init__(
        self,
        predictor: Predictor,
        upload_url: str,
    ):
        self.predictor = predictor
        self.upload_url = upload_url


    def start(self):
        print("STARTING AMQP QUEUE")
        credentials = pika.PlainCredentials('guest', 'guest')
        parameters = pika.ConnectionParameters('rabbitmq', 5672, '/', credentials)
        connection = pika.BlockingConnection(parameters)
        channel = connection.channel()

        keep_running = True  # Just hardcoding for now
        while keep_running:
            try:
                # Hardcoding the name of the test queue
                for method_frame, properties, body in channel.consume('TEST_QUEUE_NAME'):
                    message_id = properties.message_id
                    message = json.loads(body)
                    response_queue = json.loads(body)['response_queue']
                    cleanup_functions=[]  # This is supposed to be an empty list
                    self.handle_message(channel, response_queue, message, message_id, cleanup_functions)
                    channel.basic_ack(method_frame.delivery_tag)
                time.sleep(1)
            except Exception:
                tb = traceback.format_exc()
                sys.stderr.write(f"Failed to handle message: {tb}\n")

    def handle_message(self, channel, response_queue, message, message_id, cleanup_functions):
        self.predictor.setup()
        inputs = {}
        raw_inputs = message["inputs"]
        print(f'PREETHI: raw_inputs: {raw_inputs}')
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
            print(f'ALL INPUTS: {inputs}')
            inputs = validate_and_convert_inputs(
                self.predictor, inputs, cleanup_functions
            )
        except InputValidationError as e:
            tb = traceback.format_exc()
            sys.stderr.write(tb)
            self.push_error(channel, response_queue, message_id, e)
            return

        return_value = self.predictor.predict(**inputs)
        if isinstance(return_value, types.GeneratorType):
            last_result = None

            while True:
                try:
                    result = next(return_value)
                except StopIteration:
                    break
                # push the previous result, so we can eventually detect the last iteration
                if last_result is not None:
                    print('processing')
                    self.push_result(channel, response_queue, last_result, message_id, status="processing")
                if isinstance(result, Path):
                    cleanup_functions.append(result.unlink)
                last_result = result
            self.push_result(channel, response_queue, last_result, message_id, status="success")
        else:
            if isinstance(return_value, Path):
                cleanup_functions.append(return_value.unlink)
            self.push_result(channel, response_queue, return_value, message_id, status="success")

    def download(self, url):
        resp = requests.get(url)
        resp.raise_for_status()
        return resp.content

    def push_error(self, channel, response_queue, message_id, error):
        message = {
                "status": "failed",
                "error": str(error),
            }
        sys.stderr.write(f"Pushing error to {response_queue}\n")
        self.send_amqp_message(channel, response_queue, message_id, message)

    def push_result(self, channel,  response_queue, result, message_id, status):
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
        self.send_amqp_message(channel, response_queue, message_id, message)
        print('Pushed success result')

    @staticmethod
    def send_amqp_message(channel, response_queue, message_id, message_body):
        print(f'About to send amqp w message_id: {message_id}')
        channel.basic_publish(exchange='',
                              routing_key=response_queue,
                              properties=pika.BasicProperties(message_id=message_id),
                              body=json.dumps(message_body))
        print('Sent AMQP')

    def upload_to_temp(self, path: Path) -> str:
        sys.stderr.write(
            f"Uploading {path.name} to temporary storage at {self.upload_url}\n"
        )
        resp = requests.put(
            self.upload_url, files={"file": (path.name, path.open("rb"))}
        )
        resp.raise_for_status()
        return resp.json()["url"]



def _queue_worker_from_argv(
    predictor,
    redis_host
):
    return AMQPQueueWorker(
        predictor,
        redis_host
    )

if __name__ == "__main__":
    predictor = load_predictor()
    worker = _queue_worker_from_argv(predictor, *sys.argv[1:])
    worker.start()
