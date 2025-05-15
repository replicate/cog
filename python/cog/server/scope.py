import warnings
from contextlib import contextmanager
from contextvars import ContextVar
from typing import Any, Callable, Dict, Generator, Optional, Union

from attrs import evolve, frozen

from ..types import ExperimentalFeatureWarning


@frozen
class Scope:
    record_metric: Callable[[str, Union[float, int]], None]
    context: Dict[str, str] = {}
    _tag: Optional[str] = None


_current_scope: ContextVar[Optional[Scope]] = ContextVar("scope", default=None)


def current_scope() -> Scope:
    warnings.warn(
        "current_scope is an experimental internal function. It may change or be removed without warning.",
        category=ExperimentalFeatureWarning,
        stacklevel=1,
    )
    return _get_current_scope()


def _get_current_scope() -> Scope:
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


@contextmanager
def evolve_scope(**kwargs: Any) -> Generator[None, None, None]:
    new_scope = evolve(_get_current_scope(), **kwargs)
    s = _current_scope.set(new_scope)
    try:
        yield
    finally:
        _current_scope.reset(s)
