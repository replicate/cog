from contextvars import Token
from typing import Mapping

from opentelemetry.context import Context
from opentelemetry.sdk.trace.sampling import Sampler

from . import _telemetry

_CUSTOM_CONFIG_PATH = _telemetry._CUSTOM_CONFIG_PATH


def install_provider() -> None:
    _telemetry._CUSTOM_CONFIG_PATH = _CUSTOM_CONFIG_PATH
    _telemetry.install_providers()


def _http_trace_endpoint(endpoint: str, append_trace_path: bool) -> str:
    return _telemetry._http_endpoint(endpoint, append_trace_path, "traces")


def attach(carrier: Mapping[str, str]) -> Token[Context] | None:
    return _telemetry.attach(carrier)


def detach(token: Token[Context] | None) -> None:
    _telemetry.detach(token)


def shutdown() -> None:
    _telemetry.shutdown()


def _sampler() -> Sampler:
    return _telemetry._sampler()
