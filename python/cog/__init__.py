from pydantic import BaseModel

from .predictor import BasePredictor
from .types import File, Input, Path

# Backwards compatibility. Will be deprecated before 1.0.0.
Predictor = BasePredictor

__all__ = [
    "BaseModel",
    "BasePredictor",
    "File",
    "Input",
    "Path",
    "Predictor",
]
