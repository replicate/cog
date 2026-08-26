import importlib.util
import inspect
import logging
import os
import sys
import uuid
from collections.abc import Callable
from contextvars import Token
from types import ModuleType
from typing import Mapping

from opentelemetry import metrics, trace
from opentelemetry.context import (
    Context,
)
from opentelemetry.context import (
    attach as attach_context,
)
from opentelemetry.context import (
    detach as detach_context,
)
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
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

from ._version import __version__
from .telemetry import RuntimeMetricsConfig

_CUSTOM_CONFIG_PATH = "/.cog/telemetry.py"
_logger = logging.getLogger(__name__)
_tracer_provider: TracerProvider | None = None
_meter_provider: MeterProvider | None = None
_runtime_metrics_config = RuntimeMetricsConfig()


def install_providers() -> RuntimeMetricsConfig:
    global _meter_provider, _runtime_metrics_config, _tracer_provider

    if _tracer_provider is not None or _meter_provider is not None:
        return _runtime_metrics_config

    traces_enabled = _traces_enabled()
    metrics_enabled = _metrics_enabled()
    if not traces_enabled and not metrics_enabled:
        return _runtime_metrics_config

    module: ModuleType | None = None
    runtime_metrics_config = RuntimeMetricsConfig()
    tracer_provider: TracerProvider | None = None
    meter_provider: MeterProvider | None = None
    try:
        module = _load_config_from_env() if _has_custom_config() else None
        if module is not None and metrics_enabled:
            runtime_metrics_config = _read_runtime_metrics_config(module)
        resource = _base_resource()
        tracer_provider = (
            _build_tracer_provider(module, resource) if traces_enabled else None
        )
        meter_provider = (
            _build_meter_provider(module, resource) if metrics_enabled else None
        )

        _validate_tracer_provider_collision(tracer_provider)
        if tracer_provider is not None:
            trace.set_tracer_provider(tracer_provider)
            if trace.get_tracer_provider() is not tracer_provider:
                raise RuntimeError(
                    "Failed to install Cog's OpenTelemetry TracerProvider"
                )
            _tracer_provider = tracer_provider

        if meter_provider is not None:
            metrics.set_meter_provider(meter_provider)
            if metrics.get_meter_provider() is not meter_provider:
                raise RuntimeError(
                    "Failed to install Cog's OpenTelemetry MeterProvider"
                )
            _meter_provider = meter_provider

        _configure_instrumentation(module)
        _runtime_metrics_config = runtime_metrics_config
    except Exception:
        _runtime_metrics_config = RuntimeMetricsConfig()
        if tracer_provider is not _tracer_provider:
            _shutdown_provider(tracer_provider, "tracing")
        if meter_provider is not _meter_provider:
            _shutdown_provider(meter_provider, "metrics")
        shutdown()
        raise

    return _runtime_metrics_config


def runtime_metrics_config() -> RuntimeMetricsConfig:
    return _runtime_metrics_config


def attach(carrier: Mapping[str, str]) -> Token[Context] | None:
    if _tracer_provider is None or not carrier.get("traceparent"):
        return None
    context = TraceContextTextMapPropagator().extract(dict(carrier))
    return attach_context(context)


def detach(token: Token[Context] | None) -> None:
    if token is not None:
        detach_context(token)


def shutdown() -> None:
    global _meter_provider, _tracer_provider

    tracer_provider = _tracer_provider
    meter_provider = _meter_provider
    _tracer_provider = None
    _meter_provider = None

    _shutdown_provider(tracer_provider, "tracing")
    _shutdown_provider(meter_provider, "metrics")


def _shutdown_provider(
    provider: TracerProvider | MeterProvider | None, signal: str
) -> None:
    if provider is None:
        return
    try:
        provider.force_flush()
    except Exception:
        _logger.exception("Failed to flush Python %s provider", signal)
    try:
        provider.shutdown()
    except Exception:
        _logger.exception("Failed to shut down Python %s provider", signal)


def _has_custom_config() -> bool:
    return bool(os.environ.get("COG_OBSERVABILITY_CONFIG"))


def _traces_enabled() -> bool:
    return _enabled("COG_TRACE_CONFIGURED", "COG_TRACE_ENABLED") and not _sdk_disabled()


def _metrics_enabled() -> bool:
    return (
        _enabled("COG_METRICS_CONFIGURED", "COG_METRICS_ENABLED")
        and not _sdk_disabled()
    )


def _enabled(configured_name: str, enabled_name: str) -> bool:
    return os.environ.get(configured_name, "").lower() in {"1", "true", "yes"} and (
        os.environ.get(enabled_name, "true").lower() not in {"0", "false", "no"}
    )


def _sdk_disabled() -> bool:
    return os.environ.get("OTEL_SDK_DISABLED", "false").lower() in {"1", "true", "yes"}


def _load_config_from_env() -> ModuleType:
    config_path = os.environ["COG_OBSERVABILITY_CONFIG"]
    if config_path != _CUSTOM_CONFIG_PATH:
        raise RuntimeError(f"COG_OBSERVABILITY_CONFIG must be {_CUSTOM_CONFIG_PATH!r}")
    spec = importlib.util.spec_from_file_location("_cog_telemetry", config_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Cannot load observability config from {config_path!r}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _read_runtime_metrics_config(module: ModuleType) -> RuntimeMetricsConfig:
    configure = getattr(module, "configure_runtime_metrics", None)
    if configure is None:
        return RuntimeMetricsConfig()
    if not callable(configure):
        raise RuntimeError("telemetry.py configure_runtime_metrics must be callable")
    config = configure()
    if not isinstance(config, RuntimeMetricsConfig):
        raise RuntimeError(
            "telemetry.py configure_runtime_metrics must return RuntimeMetricsConfig"
        )
    return config


def _build_tracer_provider(
    module: ModuleType | None, resource: Resource
) -> TracerProvider | None:
    factory = getattr(module, "create_tracer_provider", None) if module else None
    if factory is None:
        try:
            return _create_default_tracer_provider(resource)
        except Exception:
            _logger.exception(
                "Invalid OpenTelemetry tracing configuration; tracing disabled"
            )
            return None
    if not callable(factory):
        raise RuntimeError("telemetry.py create_tracer_provider must be callable")
    provider = _call_tracer_factory(factory, resource)
    if not isinstance(provider, TracerProvider):
        raise RuntimeError(
            "telemetry.py create_tracer_provider must return TracerProvider"
        )
    return provider


def _build_meter_provider(
    module: ModuleType | None, resource: Resource
) -> MeterProvider | None:
    factory = getattr(module, "create_meter_provider", None) if module else None
    if factory is None:
        try:
            return _create_default_meter_provider(resource)
        except Exception:
            _logger.exception(
                "Invalid OpenTelemetry metrics configuration; metrics disabled"
            )
            return None
    if not callable(factory):
        raise RuntimeError("telemetry.py create_meter_provider must be callable")
    provider = factory(resource)
    if not isinstance(provider, MeterProvider):
        raise RuntimeError(
            "telemetry.py create_meter_provider must return MeterProvider"
        )
    return provider


def _call_tracer_factory(factory: Callable[..., object], resource: Resource) -> object:
    try:
        signature = inspect.signature(factory)
    except (TypeError, ValueError):
        return factory(resource)

    try:
        signature.bind(resource)
    except TypeError:
        signature.bind()
        return factory()
    return factory(resource)


def _create_default_tracer_provider(resource: Resource) -> TracerProvider | None:
    if os.environ.get("OTEL_TRACES_EXPORTER", "otlp") == "none":
        return None
    endpoint, append_path = _endpoint("traces")
    if endpoint is None:
        return None
    protocol = _protocol("traces")
    if protocol in {"http", "http/protobuf"}:
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
            OTLPSpanExporter as HttpOTLPSpanExporter,
        )

        exporter = HttpOTLPSpanExporter(
            endpoint=_http_endpoint(endpoint, append_path, "traces")
        )
    elif protocol == "grpc":
        from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
            OTLPSpanExporter as GrpcOTLPSpanExporter,
        )

        exporter = GrpcOTLPSpanExporter(endpoint=endpoint)
    else:
        raise RuntimeError(f"Unsupported OTLP protocol: {protocol}")

    provider = TracerProvider(
        resource=resource, sampler=_sampler(), shutdown_on_exit=False
    )
    provider.add_span_processor(BatchSpanProcessor(exporter))
    return provider


def _create_default_meter_provider(resource: Resource) -> MeterProvider | None:
    if os.environ.get("OTEL_METRICS_EXPORTER", "otlp") == "none":
        return None
    endpoint, append_path = _endpoint("metrics")
    if endpoint is None:
        return None
    protocol = _protocol("metrics")
    if protocol in {"http", "http/protobuf"}:
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
            OTLPMetricExporter as HttpOTLPMetricExporter,
        )

        exporter = HttpOTLPMetricExporter(
            endpoint=_http_endpoint(endpoint, append_path, "metrics")
        )
    elif protocol == "grpc":
        from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import (
            OTLPMetricExporter as GrpcOTLPMetricExporter,
        )

        exporter = GrpcOTLPMetricExporter(endpoint=endpoint)
    else:
        raise RuntimeError(f"Unsupported OTLP protocol: {protocol}")

    return MeterProvider(
        metric_readers=[PeriodicExportingMetricReader(exporter)],
        resource=resource,
        shutdown_on_exit=False,
    )


def _endpoint(signal: str) -> tuple[str | None, bool]:
    signal_endpoint = os.environ.get(f"OTEL_EXPORTER_OTLP_{signal.upper()}_ENDPOINT")
    if signal_endpoint is not None:
        return (signal_endpoint or None), False
    endpoint = os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT")
    return (endpoint or None), True


def _protocol(signal: str) -> str:
    return os.environ.get(
        f"OTEL_EXPORTER_OTLP_{signal.upper()}_PROTOCOL",
        os.environ.get("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf"),
    )


def _http_endpoint(endpoint: str, append_path: bool, signal: str) -> str:
    if not append_path:
        return endpoint
    suffix_start = min(
        (index for delimiter in "?#" if (index := endpoint.find(delimiter)) >= 0),
        default=len(endpoint),
    )
    path = endpoint[:suffix_start].rstrip("/")
    suffix = endpoint[suffix_start:]
    signal_path = f"/v1/{signal}"
    if path.endswith(signal_path):
        return f"{path}{suffix}"
    return f"{path}{signal_path}{suffix}"


def _base_resource() -> Resource:
    return Resource.create(
        {
            "service.name": os.environ.get("OTEL_SERVICE_NAME", "cog"),
            "service.version": os.environ.get(
                "COG_OBSERVABILITY_SERVICE_VERSION", __version__
            ),
            "service.instance.id": os.environ.get(
                "COG_OBSERVABILITY_INSTANCE_ID", str(uuid.uuid4())
            ),
            "cog.process.role": "worker",
        }
    )


def _validate_tracer_provider_collision(
    tracer_provider: TracerProvider | None,
) -> None:
    if tracer_provider is not None and not isinstance(
        trace.get_tracer_provider(), ProxyTracerProvider
    ):
        raise RuntimeError("A global OpenTelemetry TracerProvider is already installed")


def _configure_instrumentation(module: ModuleType | None) -> None:
    if module is None:
        return
    configure = getattr(module, "configure_instrumentation", None)
    if configure is None:
        return
    if not callable(configure):
        raise RuntimeError("telemetry.py configure_instrumentation must be callable")
    configure()


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
