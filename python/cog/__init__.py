"""
Cog SDK: Define machine learning models with standard Python.

This package provides the core types and classes for building Cog predictors.

Example:
    from cog import BaseRunner, Input, Path

    class Runner(BaseRunner):
        def setup(self):
            # Load model weights
            self.model = load_model()

        def run(
            self,
            prompt: str = Input(description="Input prompt"),
            image: Path = Input(description="Input image"),
        ) -> str:
            return self.model.generate(prompt, image)
"""

import inspect as _inspect
import sys as _sys
from collections.abc import Callable
from typing import TypeVar, overload

from coglet import CancelationException as CancelationException

from ._opaque import Opaque
from ._version import __version__
from .input import FieldInfo, Input
from .model import BaseModel
from .predictor import BasePredictor, BaseRunner
from .types import (
    AsyncConcatenateIterator,
    ConcatenateIterator,
    File,
    Path,
    Secret,
    URLFile,
    URLPath,
)

_F = TypeVar("_F", bound=Callable[..., object])


@overload
def streaming(fn: _F) -> _F:
    pass


@overload
def streaming(fn: None = None) -> Callable[[_F], _F]:
    pass


def streaming(fn: _F | None = None) -> _F | Callable[[_F], _F]:
    """Mark a predict handler as supporting streaming responses."""

    def decorate(inner: _F) -> _F:
        return inner

    if fn is None:
        return decorate
    return decorate(fn)


@overload
def concurrent(fn: _F) -> _F:
    pass


@overload
def concurrent(fn: None = None, *, max: int = 1) -> Callable[[_F], _F]:  # noqa: A002
    pass


def concurrent(
    fn: _F | None = None,
    *,
    max: int = 1,  # noqa: A002
) -> _F | Callable[[_F], _F]:
    """Configure the maximum concurrency for an async predict handler."""

    if isinstance(max, bool) or not isinstance(max, int):
        raise TypeError("concurrent max must be an integer")
    if max < 1:
        raise ValueError("concurrent max must be at least 1")

    def decorate(inner: _F) -> _F:
        if max > 1 and not (
            _inspect.iscoroutinefunction(inner) or _inspect.isasyncgenfunction(inner)
        ):
            raise TypeError("concurrent max greater than 1 requires an async function")
        inner.__cog_concurrent_max__ = max  # type: ignore[attr-defined]
        return inner

    if fn is None:
        return decorate
    if not callable(fn):
        raise TypeError(
            "concurrent must be used as @concurrent or @concurrent(max=...)"
        )
    return decorate(fn)


# ---------------------------------------------------------------------------
# Backwards-compatibility shim: ExperimentalFeatureWarning
#
# This class was removed when the Python HTTP server was replaced by coglet.
# Existing models import it to suppress warnings, e.g.:
#
#     from cog import ExperimentalFeatureWarning
#     warnings.filterwarnings("ignore", category=ExperimentalFeatureWarning)
#
# The shim keeps those models working. The stderr message is printed
# directly so it cannot be swallowed by warnings.filterwarnings("ignore").
# ---------------------------------------------------------------------------
class _ExperimentalFeatureWarning(FutureWarning):
    """Deprecated: ExperimentalFeatureWarning is no longer used by Cog.

    This class exists only for backwards compatibility. Remove the import
    and any associated ``warnings.filterwarnings(...)`` calls from your code.
    """

    pass


def __getattr__(name: str) -> object:
    if name == "ExperimentalFeatureWarning":
        print(
            "cog: ExperimentalFeatureWarning is deprecated and will be removed in a "
            "future release. Remove `ExperimentalFeatureWarning` from your imports "
            "and any associated `warnings.filterwarnings(...)` calls.",
            file=_sys.stderr,
        )
        # Cache in module namespace so __getattr__ is not called again and
        # the deprecation message prints at most once.
        globals()["ExperimentalFeatureWarning"] = _ExperimentalFeatureWarning
        return _ExperimentalFeatureWarning
    if name == "emit_metric":
        print(
            "cog: emit_metric() is deprecated and will be removed in a future release. "
            "Use current_scope().record_metric(name, value) instead.",
            file=_sys.stderr,
        )

        def emit_metric(name: str, value: float) -> None:  # noqa: A002 — name is the metric name here, not the module attr
            current_scope().record_metric(name, value)  # type: ignore[attr-defined]

        # Cache so __getattr__ is not called again — the deprecation message
        # prints at most once (on first import), not on every call.
        globals()["emit_metric"] = emit_metric
        return emit_metric
    raise AttributeError(f"module 'cog' has no attribute {name!r}")


def current_scope() -> object:
    """Get the current prediction scope for recording metrics.

    Returns a Scope object with a ``metrics`` attribute for recording
    prediction metrics. Outside a prediction context, returns a no-op scope
    that silently ignores all operations (never ``None``).

    Example::

        from cog import current_scope

        scope = current_scope()
        scope.record_metric("temperature", 0.7)
        scope.metrics["token_count"] = 42
        scope.metrics.record("logprobs", -1.2, mode="append")
    """
    import coglet

    return coglet._sdk.current_scope()  # type: ignore[attr-defined]  # PyO3 native submodule


__all__ = [
    # Version
    "__version__",
    # Core classes
    "BaseRunner",
    "BasePredictor",
    "BaseModel",
    "Opaque",
    # Input
    "Input",
    "FieldInfo",
    # Types
    "Path",
    "Secret",
    "File",
    "URLFile",
    "URLPath",
    "ConcatenateIterator",
    "AsyncConcatenateIterator",
    # Exceptions
    "CancelationException",
    # Metrics
    "current_scope",
    # Decorators
    "streaming",
    "concurrent",
    # Deprecated compat shims
    "ExperimentalFeatureWarning",
    "emit_metric",
]
