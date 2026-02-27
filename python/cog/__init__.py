"""
Cog SDK: Define machine learning models with standard Python.

This package provides the core types and classes for building Cog runners.

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

from ._version import __version__
from .coder import Coder

# Register built-in coders
from .coders import DataclassCoder, JsonCoder, SetCoder
from .exceptions import CancelationException
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


Coder.register(DataclassCoder)
Coder.register(JsonCoder)
Coder.register(SetCoder)

__all__ = [
    # Version
    "__version__",
    # Core classes
    "BaseRunner",
    "BasePredictor",  # Legacy alias for BaseRunner
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
    # Extensibility
    "Coder",
    # Exceptions
    "CancelationException",
    # Metrics
    "current_scope",
]
