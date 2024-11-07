import warnings
from contextlib import contextmanager
from contextvars import ContextVar
from typing import Callable, Generator, Optional, Union

from ..types import ExperimentalFeatureWarning


class Scope:
    def __init__(
        self,
        *,
        record_metric: Callable[[str, Union[float, int]], None],
    ) -> None:
        self.record_metric = record_metric


_current_scope: ContextVar[Optional[Scope]] = ContextVar("scope", default=None)


def current_scope() -> Scope:
    warnings.warn(
        "current_scope is an experimental internal function. It may change or be removed without warning.",
        category=ExperimentalFeatureWarning,
        stacklevel=1,
    )
    s = _current_scope.get()
    if s is None:
        raise RuntimeError("No scope available")
    return s


@contextmanager
def scope(sc: Scope) -> Generator[None, None, None]:
    s = _current_scope.set(sc)
    try:
        yield
    finally:
        _current_scope.reset(s)
