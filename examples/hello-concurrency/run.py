# Prediction interface for Cog ⚙️
# https://github.com/replicate/cog/blob/main/docs/python.md

import asyncio
import logging
import os
import time

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import (
    BatchSpanProcessor,
)

from cog import (
    AsyncConcatenateIterator,
    BaseRunner,
    Input,
    __version__,
    concurrent,
    current_scope,
)

logging.basicConfig(
    format="%(asctime)s %(levelname)-8s %(message)s",
    level=logging.INFO,
    datefmt="%Y-%m-%d %H:%M:%S",
)

honeycomb_token = ""
try:
    with open("./honeycomb_token.key", "r") as f:
        honeycomb_token = f.read().strip()
except FileNotFoundError:
    logging.info("honeycomb_token.key not found; OTEL will be disabled")

if not honeycomb_token:
    os.environ["OTEL_SDK_DISABLED"] = "true"

os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = "https://api.honeycomb.io/"
os.environ["OTEL_EXPORTER_OTLP_HEADERS"] = f"x-honeycomb-team={honeycomb_token}"
os.environ["OTEL_SERVICE_NAME"] = "cog-model"

resource = Resource(
    attributes={"model.name": "replicate/hello-concurrency", "cog_version": __version__}
)
provider = TracerProvider(resource=resource)
provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(provider)
tracer = trace.get_tracer("predict")

# Local OTEL debugging
# from opentelemetry.sdk.trace.export import ConsoleSpanExporter, SimpleSpanProcessor

# os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = http://otel-collector.local-otel.orb.local:4318
# os.environ["OTEL_SDK_DISABLED"] = ""
# provider.add_span_processor(SimpleSpanProcessor(ConsoleSpanExporter()))


class Runner(BaseRunner):
    async def setup(self) -> None:
        with tracer.start_as_current_span("setup") as span:
            self._setup_context = span.get_span_context()

            start_time = time.time()
            logging.info(f"starting setup: cog_version={__version__}")

            time.sleep(1)

            duration = time.time() - start_time
            logging.info(f"completed setup in {duration} seconds")
            span.set_attribute("model.setup_time_seconds", duration)

    @concurrent(max=4)
    async def run(  # pyright: ignore
        self,
        total: int = Input(default=5),
        interval: int = Input(default=3),
    ) -> AsyncConcatenateIterator[str]:  # pyright: ignore
        links = []
        if setup_context := getattr(self, "_setup_context", None):
            links.append(trace.Link(setup_context))

        with tracer.start_as_current_span("predict", links=links) as span:
            span.set_attribute("inputs.total", total)
            span.set_attribute("inputs.interval", interval)

            start_time = time.time()
            logging.info(
                f"starting prediction: cog_version={__version__} total={total} interval={interval}"
            )

            """Run a single prediction on the model"""
            fruits = [
                "Apple",
                "Banana",
                "Orange",
                "Grape",
                "Strawberry",
                "Mango",
                "Pineapple",
                "Blueberry",
                "Watermelon",
                "Peach",
            ][:total]

            for index, fruit in enumerate(fruits):
                if index + 1 == total:
                    yield f"{fruit}"
                else:
                    yield f"{fruit}\n"
                logging.info(f"output fruit: {fruit}")
                await asyncio.sleep(interval)

            logging.info(f"emit_metric: output_tokens={total}")
            current_scope().record_metric("output_tokens", total)
            span.set_attribute("metrics.output_tokens", total)

            duration = time.time() - start_time
            logging.info(f"completed prediction in {duration} seconds")
            span.set_attribute("model.predict_time_seconds", duration)
