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

from pydantic import ValidationError

import asyncio
from websockets import client
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


class timeout:
    """A context manager that times out after a given number of seconds."""

    def __init__(
        self,
        seconds: Optional[int],
        elapsed: Optional[int] = None,
        error_message: str = "Prediction timed out",
    ) -> None:
        if elapsed is None or seconds is None:
            self.seconds = seconds
        else:
            self.seconds = seconds - int(elapsed)
        self.error_message = error_message

    def handle_timeout(self, signum: Any, frame: Any) -> None:
        raise TimeoutError(self.error_message)

    def __enter__(self) -> None:
        if self.seconds is not None:
            if self.seconds <= 0:
                self.handle_timeout(None, None)
            else:
                signal.signal(signal.SIGALRM, self.handle_timeout)
                signal.alarm(self.seconds)

    def __exit__(self, type: Any, value: Any, traceback: Any) -> None:
        if self.seconds is not None:
            signal.alarm(0)


class WebsocketWorker:
    STAGE_SETUP = "setup"
    STAGE_RUN = "run"

    def __init__(
        self,
        predictor: BasePredictor,
        websocket_url: str,
        upload_url: str,
        model_id: Optional[str] = None,
        predict_timeout: Optional[int] = None,
    ):
        self.runner = PredictionRunner()
        self.websocket_url = websocket_url
        self.websocket_auth = os.getenv("REPLICATE_WEBSOCKET_AUTH")
        self.upload_url = upload_url
        self.model_id = model_id
        self.predict_timeout = predict_timeout
        # TODO: respect max_processing_time in message handling
        self.max_processing_time = 10 * 60  # timeout after 10 minutes
        self.running = False

        # Set up types
        self.InputType = get_input_type(predictor)

        self.should_exit = False
        self.tracer = trace.get_tracer("cog")

    def signal_exit(self, signum: Any, frame: Any) -> None:
        self.should_exit = True
        sys.stderr.write("Caught SIGTERM, exiting...\n")

    async def run(self) -> None:
        with self.tracer.start_as_current_span(name="websocket_client.setup") as span:
            signal.signal(signal.SIGTERM, self.signal_exit)
            start_time = time.time()

            # TODO(bfirsh): setup should time out too, but we don't display these logs to the user, so don't timeout to avoid confusion
            self.runner.setup()

            setup_time = time.time() - start_time
            sys.stderr.write(f"Setup time: {setup_time:.2f}\n")
        await self.ws_run()

    async def ws_run(self) -> None:
        sys.stderr.write(f"Connecting to {self.websocket_url}\n")

        async for websocket in client.connect(self.websocket_url):
            try:
                sys.stderr.write(f"Connected\n")
                async for message in websocket:
                    if message is str:
                        sys.stderr.write("processing job")
                        await self.process_message(websocket, str(message))
                    else:
                        sys.stderr.write(f"Ignoring binary payload")
            except:
                sys.stderr.write("Websocket error, will reconnect")

    async def process_message(self, websocket: client.WebSocketClientProtocol, message_json: str) -> None:
        if message_json is None:
            return

        message = json.loads(message_json)
        message_id = message["id"]
        time_in_queue = calculate_time_in_queue(message_id)  # type: ignore
        # Check whether the incoming message includes details of an
        # OpenTelemetry trace, to make distributed tracing work. The
        # value should look like:
        #
        #     00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
        if "traceparent" in message:
            context = TraceContextTextMapPropagator().extract({"traceparent": message["traceparent"]})
        else:
            context = None

        with self.tracer.start_as_current_span(
            name="websocket_client.process_message",
            context=context,
            attributes={"time_in_queue": time_in_queue},
        ) as span:
            sys.stderr.write(f"Received message {message_id} on {self.websocket_url}\n")
            # create this here so it's available during exception handling
            response: Dict[str, Any] = {"status": Status.PROCESSING, "output": None, "logs": [], "id": message_id}
            cleanup_functions: List[Callable] = []

            # This should be protected against on the server side
            if self.running:
                error = "Worker is already processing a job, cannot accept another"
                sys.stderr.write(error)
                response["status"] = Status.FAILED
                response["error"] = error
                await self.push_message(websocket, response)
                span.set_status(TraceStatus(status_code=StatusCode.ERROR))
                return
            self.running = True
            try:
                start_time = time.time()
                await self.handle_message(response, websocket, message, cleanup_functions)
                run_time = time.time() - start_time
                if not response["metrics"]:
                    response["metrics"] = {}
                response["metrics"]["predict_time"] = run_time
                sys.stderr.write(f"Run time for {message_id}: {run_time:.2f}\n")
                await self.push_message(websocket, response)
            except Exception as e:
                response["status"] = Status.FAILED
                response["error"] = str(e)
                response["x-experimental-timestamps"]["completed_at"] = datetime.datetime.now().isoformat()
                await self.push_message(websocket, response)
            finally:
                self.running = False
                for cleanup_function in cleanup_functions:
                    try:
                        cleanup_function()
                    except Exception as e:
                        sys.stderr.write(f"Cleanup function caught error: {e}")

    async def handle_message(
        self,
        websocket: client.WebSocketClientProtocol,
        response: Dict[str, Any],
        message: Dict[str, Any],
        cleanup_functions: List[Callable],
    ) -> None:
        span = trace.get_current_span()

        try:
            input_obj = self.InputType(**message["input"])
        except ValidationError as e:
            tb = traceback.format_exc()
            sys.stderr.write(tb)
            response["status"] = Status.FAILED
            response["error"] = str(e)
            await self.push_message(websocket, response)
            span.record_exception(e)
            span.set_status(TraceStatus(status_code=StatusCode.ERROR))
            return

        cleanup_functions.append(input_obj.cleanup)

        with timeout(seconds=self.predict_timeout):
            self.runner.run(**input_obj.dict())

            response["x-experimental-timestamps"] = {"started_at": datetime.datetime.now().isoformat()}

            logs: List[str] = []
            response["logs"] = logs

            await self.push_message(websocket, response)

            # just send logs until output starts
            while self.runner.is_processing() and not self.runner.has_output_waiting():
                if self.runner.has_logs_waiting():
                    logs.extend(self.runner.read_logs())
                    await self.push_message(websocket, response)

            if self.runner.error() is not None:
                response["status"] = Status.FAILED
                response["error"] = str(self.runner.error())  # type: ignore
                response["x-experimental-timestamps"]["completed_at"] = datetime.datetime.now().isoformat()
                await self.push_message(websocket, response)
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
                        new_output = [self.upload_files(o) for o in self.runner.read_output()]
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
                        await self.push_message(websocket, response)

                if self.runner.error() is not None:
                    response["status"] = Status.FAILED
                    response["error"] = str(self.runner.error())  # type: ignore
                    response["x-experimental-timestamps"]["completed_at"] = datetime.datetime.now().isoformat()
                    await self.push_message(websocket, response)
                    span.record_exception(self.runner.error())
                    span.set_status(TraceStatus(status_code=StatusCode.ERROR))
                    return

                span.add_event("received final output")

                response["status"] = Status.SUCCEEDED
                response["x-experimental-timestamps"]["completed_at"] = datetime.datetime.now().isoformat()
                output.extend(self.upload_files(o) for o in self.runner.read_output())
                logs.extend(self.runner.read_logs())
                await self.push_message(websocket, response)

            else:
                # just send logs until output ends
                while self.runner.is_processing():
                    if self.runner.has_logs_waiting():
                        logs.extend(self.runner.read_logs())
                        await self.push_message(websocket, response)

                if self.runner.error() is not None:
                    response["status"] = Status.FAILED
                    response["error"] = str(self.runner.error())  # type: ignore
                    response["x-experimental-timestamps"]["completed_at"] = datetime.datetime.now().isoformat()
                    await self.push_message(websocket, response)
                    span.record_exception(self.runner.error())
                    span.set_status(TraceStatus(status_code=StatusCode.ERROR))
                    return

                output = self.runner.read_output()
                assert len(output) == 1

                response["status"] = Status.SUCCEEDED
                response["x-experimental-timestamps"]["completed_at"] = datetime.datetime.now().isoformat()
                response["output"] = self.upload_files(output[0])
                logs.extend(self.runner.read_logs())

    def download(self, url: str) -> bytes:
        resp = requests.get(url)
        resp.raise_for_status()
        return resp.content

    async def push_message(self, websocket: client.WebSocketClientProtocol, response: Any) -> None:
        await websocket.send(json.dumps(response))

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
    try:
        now = time.time()
        queue_time = int(message_id[:13]) / 1000.0
        return now - queue_time
    except:
        return 0


def _websocket_worker_from_argv(
    predictor: BasePredictor,
    websocket_url: str,
    upload_url: str,
    model_id: str,
    predict_timeout: Optional[str] = None,
) -> WebsocketWorker:
    """
    Construct a WebsocketWorker object from sys.argv, taking into account optional arguments and types.

    This is intensely fragile. This should be kwargs or JSON or something like that.
    """
    if predict_timeout is not None:
        predict_timeout_int = int(predict_timeout)
    else:
        predict_timeout_int = None
    return WebsocketWorker(
        predictor,
        websocket_url,
        upload_url,
        model_id,
        predict_timeout_int,
    )


async def start(worker: WebsocketWorker):
    await worker.run()


if __name__ == "__main__":
    # Enable OpenTelemetry if the env vars are present. If this block isn't
    # run, all the opentelemetry calls are no-ops.
    if "OTEL_SERVICE_NAME" in os.environ:
        trace.set_tracer_provider(TracerProvider())
        span_processor = BatchSpanProcessor(OTLPSpanExporter())
        trace.get_tracer_provider().add_span_processor(span_processor)

    config = load_config()
    predictor = load_predictor(config)

    worker = _websocket_worker_from_argv(predictor, *sys.argv[1:])
    asyncio.run(start(worker))
