from .predictor import BasePredictor
from .types import File, Input, Path

# Backwards compatibility. Will be deprecated before 1.0.0.
Predictor = BasePredictor

__all__ = [
    "BasePredictor",
    "File",
    "Input",
    "Path",
    "Predictor",
]
