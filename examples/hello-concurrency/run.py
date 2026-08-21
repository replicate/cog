# Prediction interface for Cog ⚙️
# https://github.com/replicate/cog/blob/main/docs/python.md

import asyncio
import logging
import time

from opentelemetry import trace

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

tracer = trace.get_tracer(__name__)


class Runner(BaseRunner):
    async def setup(self) -> None:
        with tracer.start_as_current_span("setup") as span:
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
        with tracer.start_as_current_span("predict") as span:
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
