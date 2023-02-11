import time
from asyncio import wait_for
from os import wait

import pytest
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider

from cog.director.monitor import Monitor
from cog.schema import PredictionResponse


class MockSpanProvider:
    def __init__(self):
        self.spans = []

    def on_start(self, span, parent_context):
        self.spans.append(span)
        pass

    def on_end(self, span):
        pass

    def shutdown(*args):
        pass


def setup_span_provider():
    span_provider = MockSpanProvider()

    # use a tracing provider in test that just puts it into memory
    trace.set_tracer_provider(TracerProvider())
    trace.get_tracer_provider().add_span_processor(span_provider)

    return span_provider


def wait_for_num_spans(span_provider, num):
    start = time.perf_counter()
    while len(span_provider.spans) != num:
        time.sleep(0.01)
        if time.perf_counter() - start > 5:
            raise Exception("timed out waiting for spans")

    return span_provider.spans


def test_emit_utilization_span():
    span_provider = setup_span_provider()

    monitor = Monitor(utilization_interval=1)
    monitor.start()

    prediction = PredictionResponse(
        id="narwhal",
        input={"foo": "bar"},
        version="walrus1",
    )
    monitor.set_current_prediction(prediction)
    time.sleep(1.5)
    monitor.set_current_prediction(None)

    spans = wait_for_num_spans(span_provider, 4)

    assert spans[0].name == "cog.director.utilization"
    # Either 0.0 or 1.0, depending on whether thread gets to emit span before or
    # after `set_current_prediction` runs in the main thread
    assert "utilization" in spans[0].attributes

    assert abs(1.0 - spans[1].attributes["utilization"]) <= 0.3
    assert abs(0.5 - spans[2].attributes["utilization"]) <= 0.3

    assert spans[3].attributes["utilization"] == 0.0

    monitor.stop()
    monitor.join()
