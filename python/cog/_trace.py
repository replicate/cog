from contextvars import Token
from typing import Mapping

from opentelemetry.context import Context

from . import _telemetry


def install_provider() -> None:
    _telemetry.install_providers()


def attach(carrier: Mapping[str, str]) -> Token[Context] | None:
    return _telemetry.attach(carrier)


def detach(token: Token[Context] | None) -> None:
    _telemetry.detach(token)


def shutdown() -> None:
    _telemetry.shutdown()
