import os
import subprocess
import sys


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
    env = os.environ.copy()
    result = subprocess.run(
        [sys.executable, "-c", script],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
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
    result = subprocess.run(
        [sys.executable, "-c", script],
        env=os.environ.copy(),
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr
