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

from ._version import __version__
from .coder import Coder

# Register built-in coders
from .coders import DataclassCoder, JsonCoder, SetCoder
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


def current_scope():  # type: ignore[no-untyped-def]
    """Get the current prediction scope for recording metrics.

    Returns a Scope object with a ``metrics`` attribute for recording
    prediction metrics. Outside a prediction context, returns a no-op scope.

    Example::

        from cog import current_scope

        scope = current_scope()
        scope.record_metric("temperature", 0.7)
        scope.metrics["token_count"] = 42
        scope.metrics.record("logprobs", -1.2, mode="append")
    """
    try:
        from coglet._sdk import current_scope as _current_scope

        return _current_scope()
    except ImportError:
        # coglet not installed (e.g. running outside container) — return None
        return None


Coder.register(DataclassCoder)
Coder.register(JsonCoder)
Coder.register(SetCoder)

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
    # Extensibility
    "Coder",
    # Metrics
    "current_scope",
]
