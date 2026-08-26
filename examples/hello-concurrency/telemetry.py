from opentelemetry.context import Context
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import ReadableSpan, Span, SpanLimits, TracerProvider
from opentelemetry.sdk.trace.export import (
    BatchSpanProcessor,
    SpanProcessor,
)
from opentelemetry.sdk.trace.sampling import DEFAULT_ON

from cog.telemetry import RuntimeMetric, RuntimeMetricsConfig


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


def create_tracer_provider(resource: Resource) -> TracerProvider:
    provider = TracerProvider(
        resource=resource.merge(Resource({"model.name": "hello-concurrency"})),
        sampler=DEFAULT_ON,
        span_limits=SpanLimits(
            max_span_attributes=64,
            max_span_attribute_length=512,
            max_events=32,
        ),
        shutdown_on_exit=False,
    )
    provider.add_span_processor(ModelAttributesProcessor())

    provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))

    return provider


def create_meter_provider(resource: Resource) -> MeterProvider:
    reader = PeriodicExportingMetricReader(OTLPMetricExporter())
    return MeterProvider(
        metric_readers=[reader],
        resource=resource.merge(Resource({"model.name": "hello-concurrency"})),
        shutdown_on_exit=False,
    )


def configure_runtime_metrics() -> RuntimeMetricsConfig:
    return RuntimeMetricsConfig(disabled={RuntimeMetric.SETUP_DURATION})
