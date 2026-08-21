import importlib.util
import logging
import os
import sys
from contextvars import Token
from types import ModuleType
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
_CUSTOM_CONFIG_PATH = "/.cog/telemetry.py"
_logger = logging.getLogger(__name__)


def install_provider() -> None:
    global _provider

    if _provider is not None or not _enabled():
        return

    config_path = os.environ.get("COG_OBSERVABILITY_CONFIG")
    if not config_path and os.environ.get("OTEL_TRACES_EXPORTER", "otlp") == "none":
        return

    current = trace.get_tracer_provider()
    if not isinstance(current, ProxyTracerProvider):
        raise RuntimeError("A global OpenTelemetry TracerProvider is already installed")

    module: ModuleType | None = None
    if config_path:
        if config_path != _CUSTOM_CONFIG_PATH:
            raise RuntimeError(
                f"COG_OBSERVABILITY_CONFIG must be {_CUSTOM_CONFIG_PATH!r}"
            )
        module = _load_config(config_path)
        provider = _create_custom_provider(module)
    else:
        try:
            provider = _create_default_provider()
        except Exception:
            _logger.exception(
                "Invalid OpenTelemetry tracing configuration; tracing disabled"
            )
            return
        if provider is None:
            return

    trace.set_tracer_provider(provider)
    if trace.get_tracer_provider() is not provider:
        provider.shutdown()
        raise RuntimeError("Failed to install Cog's OpenTelemetry TracerProvider")
    _provider = provider

    if module is not None:
        configure_instrumentation = getattr(module, "configure_instrumentation", None)
        if configure_instrumentation is not None:
            if not callable(configure_instrumentation):
                shutdown()
                raise RuntimeError(
                    "telemetry.py configure_instrumentation must be callable"
                )
            try:
                configure_instrumentation()
            except Exception:
                shutdown()
                raise


def _load_config(config_path: str) -> ModuleType:
    spec = importlib.util.spec_from_file_location("_cog_telemetry", config_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Cannot load observability config from {config_path!r}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _create_custom_provider(module: ModuleType) -> TracerProvider:
    factory = getattr(module, "create_tracer_provider", None)
    if not callable(factory):
        raise RuntimeError("telemetry.py must define create_tracer_provider()")
    provider = factory()
    if not isinstance(provider, TracerProvider):
        raise RuntimeError(
            "telemetry.py create_tracer_provider() must return TracerProvider"
        )
    return provider


def _create_default_provider() -> TracerProvider | None:
    endpoint = os.environ.get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
    append_trace_path = endpoint is None
    if endpoint is None:
        endpoint = os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT", "")
    if not endpoint:
        return None

    protocol = os.environ.get(
        "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
        os.environ.get("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf"),
    )
    if protocol in {"http", "http/protobuf"}:
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
            OTLPSpanExporter as HttpOTLPSpanExporter,
        )

        exporter = HttpOTLPSpanExporter(
            endpoint=_http_trace_endpoint(endpoint, append_trace_path)
        )
    elif protocol == "grpc":
        from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
            OTLPSpanExporter as GrpcOTLPSpanExporter,
        )

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
    return provider


def _http_trace_endpoint(endpoint: str, append_trace_path: bool) -> str:
    if not append_trace_path:
        return endpoint

    suffix_start = min(
        (index for delimiter in "?#" if (index := endpoint.find(delimiter)) >= 0),
        default=len(endpoint),
    )
    path = endpoint[:suffix_start].rstrip("/")
    suffix = endpoint[suffix_start:]
    if path.endswith("/v1/traces"):
        return f"{path}{suffix}"
    return f"{path}/v1/traces{suffix}"


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
    provider = _provider
    _provider = None
    if provider is None:
        return
    try:
        provider.force_flush()
    except Exception:
        _logger.exception("Failed to flush Python tracing provider")
    try:
        provider.shutdown()
    except Exception:
        _logger.exception("Failed to shut down Python tracing provider")


def _enabled() -> bool:
    return (
        os.environ.get("COG_TRACE_CONFIGURED", "").lower() in {"1", "true", "yes"}
        and os.environ.get("COG_TRACE_ENABLED", "true").lower()
        not in {"0", "false", "no"}
        and os.environ.get("OTEL_SDK_DISABLED", "false").lower()
        not in {"1", "true", "yes"}
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
            os.environ.get("COG_TRACE_SAMPLER_ARG", "1"),
        )
    )
    if name == "traceidratio":
        return TraceIdRatioBased(ratio)
    if name == "parentbased_traceidratio":
        return ParentBasedTraceIdRatio(ratio)
    raise RuntimeError(f"Unsupported OpenTelemetry sampler: {name}")
