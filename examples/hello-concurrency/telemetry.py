import os

from opentelemetry.context import Context
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import ReadableSpan, Span, SpanLimits, TracerProvider
from opentelemetry.sdk.trace.export import (
    BatchSpanProcessor,
    ConsoleSpanExporter,
    SimpleSpanProcessor,
    SpanProcessor,
)
from opentelemetry.sdk.trace.sampling import DEFAULT_ON


class ModelAttributesProcessor(SpanProcessor):
    def on_start(
        self,
        span: Span,
        parent_context: Context | None = None,
    ) -> None:
        span.set_attribute("model.name", "replicate/hello-concurrency")

    def on_end(self, span: ReadableSpan) -> None:
        pass

    def shutdown(self) -> None:
        pass

    def force_flush(self, timeout_millis: int = 30_000) -> bool:
        return True


def create_tracer_provider() -> TracerProvider:
    provider = TracerProvider(
        resource=Resource.create(
            {"service.name": os.getenv("OTEL_SERVICE_NAME", "hello-concurrency")}
        ),
        sampler=DEFAULT_ON,
        span_limits=SpanLimits(
            max_span_attributes=64,
            max_span_attribute_length=512,
            max_events=32,
        ),
        shutdown_on_exit=False,
    )
    provider.add_span_processor(ModelAttributesProcessor())

    if os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT"):
        provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
    if os.getenv("OTEL_DEBUG_TRACES", "false").lower() == "true":
        provider.add_span_processor(SimpleSpanProcessor(ConsoleSpanExporter()))

    return provider
