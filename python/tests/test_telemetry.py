import os
import subprocess
import sys
from pathlib import Path


def _run_script(script: str) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    for name in list(env):
        if name.startswith(
            ("COG_OBSERVABILITY_", "COG_TRACE_", "COG_METRICS_", "OTEL_")
        ):
            del env[name]
    return subprocess.run(
        [sys.executable, "-c", script],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )


def test_default_meter_provider_installs() -> None:
    result = _run_script(
        """
import os
os.environ.update({
    "COG_METRICS_CONFIGURED": "true",
    "COG_METRICS_ENABLED": "true",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
})
from cog import _telemetry
from cog.telemetry import RuntimeMetric
from opentelemetry import metrics
from opentelemetry.sdk.metrics import MeterProvider
_telemetry.install_providers()
assert isinstance(metrics.get_meter_provider(), MeterProvider)
"""
    )
    assert result.returncode == 0, result.stderr


def test_http_metrics_endpoint_appends_signal_path_once() -> None:
    result = _run_script(
        """
from cog import _telemetry
assert _telemetry._http_endpoint("https://collector:4318", True, "metrics") == "https://collector:4318/v1/metrics"
assert _telemetry._http_endpoint("https://collector:4318/v1/metrics", True, "metrics") == "https://collector:4318/v1/metrics"
assert _telemetry._http_endpoint("https://collector:4318/base?token=secret", True, "metrics") == "https://collector:4318/base/v1/metrics?token=secret"
"""
    )
    assert result.returncode == 0, result.stderr


def test_worker_resource_uses_the_process_instance_id() -> None:
    result = _run_script(
        """
import os
os.environ["COG_OBSERVABILITY_INSTANCE_ID"] = "worker-instance"
os.environ["COG_OBSERVABILITY_SERVICE_VERSION"] = "worker-version"
from cog import _telemetry
assert _telemetry._base_resource().attributes["service.instance.id"] == "worker-instance"
assert _telemetry._base_resource().attributes["service.version"] == "worker-version"
"""
    )
    assert result.returncode == 0, result.stderr


def test_worker_resource_attributes_override_cog_defaults() -> None:
    result = _run_script(
        """
import os
os.environ["OTEL_RESOURCE_ATTRIBUTES"] = "service.name=resource-service,service.version=resource-version,service.instance.id=resource-instance,cog.process.role=invalid"
os.environ["OTEL_SERVICE_NAME"] = "service-name-override"
from cog import _telemetry
resource = _telemetry._base_resource()
assert resource.attributes["service.name"] == "service-name-override"
assert resource.attributes["service.version"] == "resource-version"
assert resource.attributes["service.instance.id"] == "resource-instance"
assert resource.attributes["cog.process.role"] == "worker"
"""
    )
    assert result.returncode == 0, result.stderr


def test_custom_meter_provider_and_runtime_metric_config(tmp_path: Path) -> None:
    config = tmp_path / "telemetry.py"
    config.write_text(
        """
from cog.telemetry import RuntimeMetric, RuntimeMetricsConfig
from opentelemetry.sdk.metrics import MeterProvider

def create_meter_provider(resource):
    assert resource.attributes["cog.process.role"] == "worker"
    return MeterProvider(shutdown_on_exit=False)

def configure_runtime_metrics():
    return RuntimeMetricsConfig(disabled={RuntimeMetric.SETUP_DURATION})
"""
    )
    result = _run_script(
        f"""
import os
os.environ.update({{
    "COG_METRICS_CONFIGURED": "true",
    "COG_METRICS_ENABLED": "true",
    "COG_OBSERVABILITY_CONFIG": {str(config)!r},
    "OTEL_METRICS_EXPORTER": "none",
}})
from cog import _telemetry
from cog.telemetry import RuntimeMetric
from opentelemetry import metrics
from opentelemetry.sdk.metrics import MeterProvider
_telemetry._CUSTOM_CONFIG_PATH = {str(config)!r}
config = _telemetry.install_providers()
assert isinstance(metrics.get_meter_provider(), MeterProvider)
assert config.disabled == {{RuntimeMetric.SETUP_DURATION}}
"""
    )
    assert result.returncode == 0, result.stderr


def test_shutdown_exports_metrics_without_a_periodic_thread() -> None:
    result = _run_script(
        """
import math
from cog import _telemetry
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import (
    MetricExporter, MetricExportResult, PeriodicExportingMetricReader,
)

class Exporter(MetricExporter):
    def __init__(self):
        super().__init__()
        self.values = []
        self.closed = False

    def export(self, data, **kwargs):
        for resource in data.resource_metrics:
            for scope in resource.scope_metrics:
                for metric in scope.metrics:
                    self.values.extend(point.value for point in metric.data.data_points)
        return MetricExportResult.SUCCESS

    def force_flush(self, **kwargs):
        return True

    def shutdown(self, **kwargs):
        self.closed = True

exporter = Exporter()
reader = PeriodicExportingMetricReader(exporter, export_interval_millis=math.inf)
provider = MeterProvider(metric_readers=[reader], shutdown_on_exit=False)
provider.get_meter("test").create_counter("requests").add(3)
_telemetry._meter_provider = provider
_telemetry.shutdown()
assert exporter.values == [3], exporter.values
assert exporter.closed
"""
    )
    assert result.returncode == 0, result.stderr


def test_shutdown_closes_provider_when_flush_fails() -> None:
    result = _run_script(
        """
from cog import _telemetry
from opentelemetry.sdk.metrics import MeterProvider

class Provider(MeterProvider):
    closed = False

    def force_flush(self, **kwargs):
        raise RuntimeError("flush failed")

    def shutdown(self, **kwargs):
        self.closed = True

provider = Provider(shutdown_on_exit=False)
_telemetry._meter_provider = provider
_telemetry.shutdown()
assert provider.closed
"""
    )
    assert result.returncode == 0, result.stderr
    assert "flush failed" in result.stderr


def test_invalid_meter_configuration_stops_reader_thread() -> None:
    result = _run_script(
        """
import os
import threading
os.environ.update({
    "COG_METRICS_CONFIGURED": "true",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318",
    "OTEL_METRICS_EXEMPLAR_FILTER": "invalid",
})
from cog import _telemetry
_telemetry.install_providers()
assert _telemetry._meter_provider is None
assert not any(t.name == "OtelPeriodicExportingMetricReader" for t in threading.enumerate())
"""
    )
    assert result.returncode == 0, result.stderr


def test_legacy_tracer_factory_keeps_optional_argument_default() -> None:
    result = _run_script(
        """
from cog import _telemetry
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.sampling import ALWAYS_ON

def factory(sampler=ALWAYS_ON):
    return TracerProvider(sampler=sampler, shutdown_on_exit=False)

provider = _telemetry._call_provider_factory(factory, _telemetry._base_resource())
with provider.get_tracer("test").start_as_current_span("test") as span:
    assert span.is_recording()
provider.shutdown()
"""
    )
    assert result.returncode == 0, result.stderr


def test_invalid_runtime_metrics_configuration_fails_setup(tmp_path: Path) -> None:
    config = tmp_path / "telemetry.py"
    config.write_text("def configure_runtime_metrics():\n    return object()\n")
    result = _run_script(
        f"""
import os
os.environ.update({{
    "COG_METRICS_CONFIGURED": "true",
    "COG_METRICS_ENABLED": "true",
    "COG_OBSERVABILITY_CONFIG": {str(config)!r},
}})
from cog import _telemetry
_telemetry._CUSTOM_CONFIG_PATH = {str(config)!r}
_telemetry.install_providers()
"""
    )
    assert result.returncode != 0
    assert "must return RuntimeMetricsConfig" in result.stderr


def test_constructed_provider_is_closed_when_second_factory_fails(
    tmp_path: Path,
) -> None:
    marker = tmp_path / "closed"
    config = tmp_path / "telemetry.py"
    config.write_text(
        f"""
from pathlib import Path
from opentelemetry.sdk.trace import TracerProvider

marker = Path({str(marker)!r})

class Provider(TracerProvider):
    def force_flush(self, timeout_millis=30000):
        return True

    def shutdown(self):
        marker.touch()

def create_tracer_provider(resource):
    return Provider(shutdown_on_exit=False)

def create_meter_provider(resource):
    raise RuntimeError("meter factory failed")
"""
    )
    result = _run_script(
        f"""
import os
os.environ.update({{
    "COG_TRACE_CONFIGURED": "true",
    "COG_TRACE_ENABLED": "true",
    "COG_METRICS_CONFIGURED": "true",
    "COG_METRICS_ENABLED": "true",
    "COG_OBSERVABILITY_CONFIG": {str(config)!r},
}})
from cog import _telemetry
_telemetry._CUSTOM_CONFIG_PATH = {str(config)!r}
_telemetry.install_providers()
"""
    )
    assert result.returncode != 0
    assert "meter factory failed" in result.stderr
    assert marker.exists()


def test_disabled_metrics_do_not_load_telemetry_config(tmp_path: Path) -> None:
    marker = tmp_path / "imported"
    config = tmp_path / "telemetry.py"
    config.write_text(f"from pathlib import Path\nPath({str(marker)!r}).touch()\n")
    result = _run_script(
        f"""
import os
os.environ.update({{
    "COG_METRICS_CONFIGURED": "true",
    "COG_METRICS_ENABLED": "false",
    "COG_OBSERVABILITY_CONFIG": {str(config)!r},
}})
from cog import _telemetry
_telemetry._CUSTOM_CONFIG_PATH = {str(config)!r}
_telemetry.install_providers()
"""
    )
    assert result.returncode == 0, result.stderr
    assert not marker.exists()
