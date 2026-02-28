"""
Cog SDK: Define machine learning models with standard Python.

This package provides the core types and classes for building Cog predictors.

Example:
    from cog import BasePredictor, Input, Path

    class Predictor(BasePredictor):
        def setup(self):
            # Load model weights
            self.model = load_model()

        def predict(
            self,
            prompt: str = Input(description="Input prompt"),
            image: Path = Input(description="Input image"),
        ) -> str:
            return self.model.generate(prompt, image)
"""

import sys as _sys

from coglet import CancelationException as CancelationException

from ._version import __version__
from .input import FieldInfo, Input
from .model import BaseModel
from .predictor import BasePredictor
from .types import (
    AsyncConcatenateIterator,
    ConcatenateIterator,
    File,
    Path,
    Secret,
    URLFile,
    URLPath,
)


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
    "BasePredictor",
    "BaseModel",
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
    # Deprecated compat shims
    "ExperimentalFeatureWarning",
]
