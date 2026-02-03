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

# Register built-in coders
from .coders import DataclassCoder, JsonCoder, SetCoder

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
]
