import contextlib
import importlib.util
import inspect
import logging
import os
import sys
import threading
import time
import uuid
from collections.abc import Callable
from contextvars import Token
from types import ModuleType
from typing import Mapping

from opentelemetry import _logs, metrics, trace
from opentelemetry.attributes import _VALID_ANY_VALUE_TYPES
from opentelemetry.context import (
    Context,
    get_current,
)
from opentelemetry.context import (
    attach as attach_context,
)
from opentelemetry.context import (
    detach as detach_context,
)
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs._internal import std_to_otel
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import OTELResourceDetector, Resource
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
from opentelemetry.util.types import AnyValue

from ._version import __version__
from .telemetry import RuntimeMetricsConfig

_CUSTOM_CONFIG_PATH = "/.cog/telemetry.py"
_logger = logging.getLogger(__name__)
_tracer_provider: TracerProvider | None = None
_meter_provider: MeterProvider | None = None
_logger_provider: LoggerProvider | None = None
_runtime_metrics_config = RuntimeMetricsConfig()
_original_call_handlers = logging.Logger.callHandlers
_exporting_log = threading.local()
# Python level names that differ from OTel severity text.
_SEVERITY_TEXT = {"WARNING": "WARN", "CRITICAL": "FATAL"}


def install_providers() -> RuntimeMetricsConfig:
    global _logger_provider, _meter_provider, _runtime_metrics_config, _tracer_provider

    if (
        _tracer_provider is not None
        or _meter_provider is not None
        or _logger_provider is not None
    ):
        return _runtime_metrics_config

    traces_enabled = _traces_enabled()
    metrics_enabled = _metrics_enabled()
    logs_enabled = _logs_enabled()
    if not traces_enabled and not metrics_enabled and not logs_enabled:
        return _runtime_metrics_config

    module: ModuleType | None = None
    _runtime_metrics_config = RuntimeMetricsConfig()
    tracer_provider: TracerProvider | None = None
    meter_provider: MeterProvider | None = None
    logger_provider: LoggerProvider | None = None
    try:
        module = _load_config_from_env() if _has_custom_config() else None
        if module is not None and metrics_enabled:
            _runtime_metrics_config = _read_runtime_metrics_config(module)
        resource = _base_resource()
        tracer_provider = (
            _build_tracer_provider(module, resource) if traces_enabled else None
        )
        meter_provider = (
            _build_meter_provider(module, resource) if metrics_enabled else None
        )
        logger_provider = (
            _build_logger_provider(module, resource) if logs_enabled else None
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

        if logger_provider is not None:
            _logs.set_logger_provider(logger_provider)
            if _logs.get_logger_provider() is not logger_provider:
                raise RuntimeError(
                    "Failed to install Cog's OpenTelemetry LoggerProvider"
                )
            _logger_provider = logger_provider
            logging.Logger.callHandlers = _call_handlers_and_export  # type: ignore[method-assign]

        _configure_instrumentation(module)
    except Exception:
        if tracer_provider is not _tracer_provider:
            _shutdown_provider(tracer_provider, "tracing")
        if meter_provider is not _meter_provider:
            _shutdown_provider(meter_provider, "metrics")
        if logger_provider is not _logger_provider:
            _shutdown_provider(logger_provider, "logs")
        shutdown()
        raise

    return _runtime_metrics_config


def get_runtime_metrics_config() -> RuntimeMetricsConfig:
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
    global _logger_provider, _meter_provider, _tracer_provider

    tracer_provider = _tracer_provider
    meter_provider = _meter_provider
    logger_provider = _logger_provider
    _tracer_provider = None
    _meter_provider = None
    _logger_provider = None
    if logging.Logger.callHandlers is _call_handlers_and_export:
        logging.Logger.callHandlers = _original_call_handlers  # type: ignore[method-assign]

    _shutdown_provider(tracer_provider, "tracing")
    _shutdown_provider(meter_provider, "metrics")
    _shutdown_provider(logger_provider, "logs")


def _call_handlers_and_export(self: logging.Logger, record: logging.LogRecord) -> None:
    # Runs after the logger's disabled and filter checks and before handlers, so
    # export follows the model's logging config without adding a handler. A
    # root handler would stop logging.lastResort and make basicConfig() a no-op.
    _export_log_record(record)
    _original_call_handlers(self, record)


def _export_log_record(record: logging.LogRecord) -> None:
    provider = _logger_provider
    if (
        provider is None
        or record.name.startswith("opentelemetry")
        or getattr(_exporting_log, "active", False)
        # OTel SDK export threads: an exporter's HTTP or gRPC client logging a
        # failed export would otherwise queue another record for export.
        or threading.current_thread().name.startswith("Otel")
    ):
        return
    _exporting_log.active = True
    try:
        # Logging an export failure here would re-enter this hook.
        with contextlib.suppress(Exception):
            provider.get_logger(record.name).emit(
                _logs.LogRecord(
                    timestamp=int(record.created * 1e9),
                    observed_timestamp=time.time_ns(),
                    context=get_current(),
                    severity_text=_SEVERITY_TEXT.get(
                        record.levelname, record.levelname
                    ),
                    severity_number=std_to_otel(record.levelno),
                    body=_log_body(record),
                    # A staticmethod, so this reuses the SDK's attribute mapping
                    # without constructing the deprecated handler.
                    attributes=LoggingHandler._get_attributes(record),
                )
            )
    finally:
        _exporting_log.active = False


def _log_body(record: logging.LogRecord) -> AnyValue:
    # Matches the SDK handler: a non-string msg without format args, such as a
    # dict, stays structured instead of being converted to a string.
    if record.args or isinstance(record.msg, str):
        return record.getMessage()
    if isinstance(record.msg, _VALID_ANY_VALUE_TYPES):
        return record.msg
    return str(record.msg)


def _shutdown_provider(
    provider: TracerProvider | MeterProvider | LoggerProvider | None, signal: str
) -> None:
    if provider is None:
        return
    try:
        provider.shutdown()
    except Exception:
        _logger.exception("Failed to shut down Python %s provider", signal)


def _has_custom_config() -> bool:
    return bool(os.environ.get("COG_OBSERVABILITY_CONFIG"))


def _traces_enabled() -> bool:
    return _enabled("COG_TRACE_CONFIGURED", "COG_TRACE_ENABLED")


def _metrics_enabled() -> bool:
    return _enabled("COG_METRICS_CONFIGURED", "COG_METRICS_ENABLED")


def _logs_enabled() -> bool:
    return _enabled("COG_LOGS_CONFIGURED", "COG_LOGS_ENABLED")


def _enabled(configured_name: str, enabled_name: str) -> bool:
    try:
        return (
            _env_bool(configured_name, False)
            and _env_bool(enabled_name, True)
            and not _env_bool("OTEL_SDK_DISABLED", False)
        )
    except ValueError as error:
        _logger.warning("%s; %s disabled", error, enabled_name)
        return False


def _env_bool(name: str, default: bool) -> bool:
    value = os.environ.get(name)
    if value is None:
        return default
    if value.lower() in {"1", "true", "yes"}:
        return True
    if value.lower() in {"0", "false", "no"}:
        return False
    raise ValueError(f"{name} must be true or false")


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
    provider = _call_provider_factory(factory, resource)
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
    provider = _call_provider_factory(factory, resource)
    if not isinstance(provider, MeterProvider):
        raise RuntimeError(
            "telemetry.py create_meter_provider must return MeterProvider"
        )
    return provider


def _build_logger_provider(
    module: ModuleType | None, resource: Resource
) -> LoggerProvider | None:
    factory = getattr(module, "create_logger_provider", None) if module else None
    if factory is None:
        try:
            return _create_default_logger_provider(resource)
        except Exception:
            _logger.exception("Invalid OpenTelemetry logs configuration; logs disabled")
            return None
    if not callable(factory):
        raise RuntimeError("telemetry.py create_logger_provider must be callable")
    provider = _call_provider_factory(factory, resource)
    if not isinstance(provider, LoggerProvider):
        raise RuntimeError(
            "telemetry.py create_logger_provider must return LoggerProvider"
        )
    return provider


def _call_provider_factory(
    factory: Callable[..., object], resource: Resource
) -> object:
    try:
        signature = inspect.signature(factory)
    except (TypeError, ValueError):
        return factory(resource)

    try:
        signature.bind()
    except TypeError:
        signature.bind(resource)
        return factory(resource)
    return factory()


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

    reader = PeriodicExportingMetricReader(exporter)
    try:
        return MeterProvider(
            metric_readers=[reader],
            resource=resource,
            shutdown_on_exit=False,
        )
    except Exception:
        reader.shutdown()
        raise


def _create_default_logger_provider(resource: Resource) -> LoggerProvider | None:
    if os.environ.get("OTEL_LOGS_EXPORTER", "otlp") == "none":
        return None
    endpoint, append_path = _endpoint("logs")
    if endpoint is None:
        return None
    protocol = _protocol("logs")
    if protocol in {"http", "http/protobuf"}:
        from opentelemetry.exporter.otlp.proto.http._log_exporter import (
            OTLPLogExporter as HttpOTLPLogExporter,
        )

        exporter = HttpOTLPLogExporter(
            endpoint=_http_endpoint(endpoint, append_path, "logs")
        )
    elif protocol == "grpc":
        from opentelemetry.exporter.otlp.proto.grpc._log_exporter import (
            OTLPLogExporter as GrpcOTLPLogExporter,
        )

        exporter = GrpcOTLPLogExporter(endpoint=endpoint)
    else:
        raise RuntimeError(f"Unsupported OTLP protocol: {protocol}")

    provider = LoggerProvider(resource=resource, shutdown_on_exit=False)
    provider.add_log_record_processor(BatchLogRecordProcessor(exporter))
    return provider


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
    resource = Resource.create(
        {
            "service.name": "cog",
            "service.version": os.environ.get(
                "COG_OBSERVABILITY_SERVICE_VERSION", __version__
            ),
            "service.instance.id": os.environ.get(
                "COG_OBSERVABILITY_INSTANCE_ID", str(uuid.uuid4())
            ),
        }
    )
    resource = resource.merge(OTELResourceDetector().detect())
    return resource.merge(Resource({"cog.process.role": "worker"}))


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
