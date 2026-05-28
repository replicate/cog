from collections.abc import Callable
from inspect import isasyncgenfunction, iscoroutinefunction
from typing import TypeVar, overload

_F = TypeVar("_F", bound=Callable[..., object])


@overload
def concurrent(fn: _F) -> _F:
    pass


@overload
def concurrent(fn: None = None, *, max: int = 1) -> Callable[[_F], _F]:
    pass


def concurrent(fn: _F | None = None, *, max: int = 1) -> _F | Callable[[_F], _F]:
    """Mark a run/predict function as safe for concurrent execution."""

    if type(max) is not int:
        raise TypeError("max must be an integer")
    if max < 1:
        raise ValueError("max must be at least 1")

    def decorate(inner: _F) -> _F:
        if max > 1 and not (iscoroutinefunction(inner) or isasyncgenfunction(inner)):
            raise TypeError("max > 1 requires an async function")
        inner.__cog_concurrent_max__ = max  # type: ignore[attr-defined]
        return inner

    if fn is None:
        return decorate
    return decorate(fn)
