import os
from contextvars import Token
from typing import Mapping

from opentelemetry import trace
from opentelemetry.context import (
    Context,
)
from opentelemetry.context import (
    attach as attach_context,
)
from opentelemetry.context import (
    detach as detach_context,
)
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
    OTLPSpanExporter as GrpcOTLPSpanExporter,
)
from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
    OTLPSpanExporter as HttpOTLPSpanExporter,
)
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.sampling import (
    ALWAYS_OFF,
    ALWAYS_ON,
    DEFAULT_OFF,
    DEFAULT_ON,
    ParentBasedTraceIdRatio,
    Sampler,
    TraceIdRatioBased,
)
from opentelemetry.trace import ProxyTracerProvider
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

_provider: TracerProvider | None = None


def install_provider() -> None:
    global _provider

    if _provider is not None or not _enabled():
        return

    current = trace.get_tracer_provider()
    if not isinstance(current, ProxyTracerProvider):
        raise RuntimeError("A global OpenTelemetry TracerProvider is already installed")

    endpoint = os.environ.get(
        "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
        os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
    ).rstrip("/")
    if not endpoint:
        return

    protocol = os.environ.get(
        "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
        os.environ.get("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf"),
    )
    if protocol in {"http", "http/protobuf"}:
        exporter = HttpOTLPSpanExporter(endpoint=f"{endpoint}/v1/traces")
    elif protocol == "grpc":
        exporter = GrpcOTLPSpanExporter(endpoint=endpoint)
    else:
        raise RuntimeError(f"Unsupported OTLP protocol: {protocol}")

    resource = Resource.create(
        {
            "service.name": os.environ.get("OTEL_SERVICE_NAME", "cog"),
            "cog.process.role": "worker",
        }
    )
    provider = TracerProvider(
        resource=resource,
        sampler=_sampler(),
        shutdown_on_exit=False,
    )
    provider.add_span_processor(BatchSpanProcessor(exporter))
    trace.set_tracer_provider(provider)
    if trace.get_tracer_provider() is not provider:
        provider.shutdown()
        raise RuntimeError("Failed to install Cog's OpenTelemetry TracerProvider")
    _provider = provider


def attach(carrier: Mapping[str, str]) -> Token[Context] | None:
    if _provider is None or not carrier.get("traceparent"):
        return None
    context = TraceContextTextMapPropagator().extract(dict(carrier))
    return attach_context(context)


def detach(token: Token[Context] | None) -> None:
    if token is not None:
        detach_context(token)


def shutdown() -> None:
    global _provider
    if _provider is None:
        return
    _provider.force_flush()
    _provider.shutdown()
    _provider = None


def _enabled() -> bool:
    return (
        os.environ.get("COG_TRACE_CONFIGURED", "").lower() in {"1", "true", "yes"}
        and os.environ.get("COG_TRACE_ENABLED", "true").lower()
        not in {"0", "false", "no"}
        and os.environ.get("OTEL_SDK_DISABLED", "false").lower()
        not in {"1", "true", "yes"}
        and os.environ.get("OTEL_TRACES_EXPORTER", "otlp") != "none"
    )


def _sampler() -> Sampler:
    name = os.environ.get(
        "OTEL_TRACES_SAMPLER",
        os.environ.get("COG_TRACE_SAMPLER", "parentbased_always_off"),
    )
    if name == "always_on":
        return ALWAYS_ON
    if name == "always_off":
        return ALWAYS_OFF
    if name == "parentbased_always_on":
        return DEFAULT_ON
    if name == "parentbased_always_off":
        return DEFAULT_OFF

    ratio = float(
        os.environ.get(
            "OTEL_TRACES_SAMPLER_ARG",
            os.environ.get("COG_TRACE_SAMPLER_ARG", ""),
        )
    )
    if name == "traceidratio":
        return TraceIdRatioBased(ratio)
    if name == "parentbased_traceidratio":
        return ParentBasedTraceIdRatio(ratio)
    raise RuntimeError(f"Unsupported OpenTelemetry sampler: {name}")
