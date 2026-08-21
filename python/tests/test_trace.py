import os
import subprocess
import sys
from pathlib import Path


def _run_script(script: str) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    for name in list(env):
        if name.startswith(("COG_OBSERVABILITY_", "COG_TRACE_", "OTEL_")):
            del env[name]
    return subprocess.run(
        [sys.executable, "-c", script],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )


def test_trace_provider_installs_before_model_import() -> None:
    script = """
import os
os.environ.update({
    "COG_TRACE_CONFIGURED": "true",
    "COG_TRACE_ENABLED": "true",
    "COG_TRACE_SAMPLER": "parentbased_always_off",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
})
from cog import _trace
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
_trace.install_provider()
assert isinstance(trace.get_tracer_provider(), TracerProvider)
"""
    result = _run_script(script)
    assert result.returncode == 0, result.stderr


def test_ratio_sampler_without_arg_defaults_to_one() -> None:
    script = """
import os
os.environ["OTEL_TRACES_SAMPLER"] = "traceidratio"
from cog import _trace
from opentelemetry.sdk.trace.sampling import TraceIdRatioBased
sampler = _trace._sampler()
assert isinstance(sampler, TraceIdRatioBased)
assert sampler.rate == 1.0
"""
    result = _run_script(script)
    assert result.returncode == 0, result.stderr


def test_http_trace_endpoint_appends_signal_path_once() -> None:
    script = """
from cog import _trace
assert _trace._http_trace_endpoint("https://collector:4318", True) == "https://collector:4318/v1/traces"
assert _trace._http_trace_endpoint("https://collector:4318/v1/traces", True) == "https://collector:4318/v1/traces"
assert _trace._http_trace_endpoint("https://collector:4318/base?token=secret", True) == "https://collector:4318/base/v1/traces?token=secret"
assert _trace._http_trace_endpoint("https://collector:4318/custom?token=secret", False) == "https://collector:4318/custom?token=secret"
"""
    result = _run_script(script)
    assert result.returncode == 0, result.stderr


def test_invalid_default_trace_config_disables_tracing() -> None:
    script = """
import os
os.environ.update({
    "COG_TRACE_CONFIGURED": "true",
    "COG_TRACE_ENABLED": "true",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "https",
})
from cog import _trace
from opentelemetry import trace
from opentelemetry.trace import ProxyTracerProvider
_trace.install_provider()
assert isinstance(trace.get_tracer_provider(), ProxyTracerProvider)
"""
    result = _run_script(script)
    assert result.returncode == 0, result.stderr


def test_trace_provider_rejects_collision() -> None:
    script = """
import os
os.environ.update({
    "COG_TRACE_CONFIGURED": "true",
    "COG_TRACE_ENABLED": "true",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318",
})
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
trace.set_tracer_provider(TracerProvider())
from cog import _trace
try:
    _trace.install_provider()
except RuntimeError:
    pass
else:
    raise AssertionError("expected provider collision")
"""
    result = _run_script(script)
    assert result.returncode == 0, result.stderr


def test_disabled_builtin_exporter_allows_existing_provider() -> None:
    script = """
import os
os.environ.update({
    "COG_TRACE_CONFIGURED": "true",
    "COG_TRACE_ENABLED": "true",
    "OTEL_TRACES_EXPORTER": "none",
})
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
provider = TracerProvider()
trace.set_tracer_provider(provider)
from cog import _trace
_trace.install_provider()
assert trace.get_tracer_provider() is provider
"""
    result = _run_script(script)
    assert result.returncode == 0, result.stderr


def test_custom_trace_provider_and_instrumentation(tmp_path: Path) -> None:
    marker = tmp_path / "lifecycle.txt"
    config = tmp_path / "telemetry.py"
    config.write_text(
        f"""
from pathlib import Path
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider

marker = Path({str(marker)!r})

class Provider(TracerProvider):
    def force_flush(self, timeout_millis=30000):
        marker.write_text(marker.read_text() + "flush\\n")
        return True

    def shutdown(self):
        marker.write_text(marker.read_text() + "shutdown\\n")

def create_tracer_provider():
    return Provider(shutdown_on_exit=False)

def configure_instrumentation():
    assert isinstance(trace.get_tracer_provider(), Provider)
    marker.write_text("configured\\n")
"""
    )
    script = f"""
import os
os.environ.update({{
    "COG_TRACE_CONFIGURED": "true",
    "COG_TRACE_ENABLED": "true",
    "COG_OBSERVABILITY_CONFIG": {str(config)!r},
    "OTEL_TRACES_EXPORTER": "none",
}})
from cog import _trace
_trace._CUSTOM_CONFIG_PATH = {str(config)!r}
_trace.install_provider()
_trace.shutdown()
"""

    result = _run_script(script)
    assert result.returncode == 0, result.stderr
    assert marker.read_text() == "configured\nflush\nshutdown\n"


def test_custom_trace_provider_errors(tmp_path: Path) -> None:
    tests = {
        "missing factory": ("value = True\n", "must define create_tracer_provider"),
        "wrong provider": (
            "def create_tracer_provider():\n    return object()\n",
            "must return TracerProvider",
        ),
        "invalid instrumentation": (
            "from opentelemetry.sdk.trace import TracerProvider\n"
            "def create_tracer_provider():\n    return TracerProvider(shutdown_on_exit=False)\n"
            "configure_instrumentation = True\n",
            "configure_instrumentation must be callable",
        ),
        "instrumentation failure": (
            "from opentelemetry.sdk.trace import TracerProvider\n"
            "def create_tracer_provider():\n    return TracerProvider(shutdown_on_exit=False)\n"
            "def configure_instrumentation():\n    raise RuntimeError('instrumentation failed')\n",
            "instrumentation failed",
        ),
    }

    for name, (contents, expected_error) in tests.items():
        config = tmp_path / f"{name.replace(' ', '_')}.py"
        config.write_text(contents)
        script = f"""
import os
os.environ.update({{
    "COG_TRACE_CONFIGURED": "true",
    "COG_TRACE_ENABLED": "true",
    "COG_OBSERVABILITY_CONFIG": {str(config)!r},
}})
from cog import _trace
_trace._CUSTOM_CONFIG_PATH = {str(config)!r}
_trace.install_provider()
"""
        result = _run_script(script)
        assert result.returncode != 0, name
        assert expected_error in result.stderr, result.stderr


def test_custom_trace_provider_honors_disable_switches(tmp_path: Path) -> None:
    marker = tmp_path / "imported"
    config = tmp_path / "telemetry.py"
    config.write_text(f"from pathlib import Path\nPath({str(marker)!r}).touch()\n")

    for name, value in [
        ("COG_TRACE_ENABLED", "false"),
        ("OTEL_SDK_DISABLED", "true"),
    ]:
        script = f"""
import os
os.environ.update({{
    "COG_TRACE_CONFIGURED": "true",
    "COG_TRACE_ENABLED": "true",
    "COG_OBSERVABILITY_CONFIG": {str(config)!r},
    {name!r}: {value!r},
}})
from cog import _trace
_trace._CUSTOM_CONFIG_PATH = {str(config)!r}
_trace.install_provider()
"""
        result = _run_script(script)
        assert result.returncode == 0, result.stderr
        assert not marker.exists()
