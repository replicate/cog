try:
    from pydantic.v1 import BaseModel  # type: ignore
except ImportError:
    from pydantic import BaseModel  # pylint: disable=W0404


from .predictor import BasePredictor
from .types import ConcatenateIterator, File, Input, Path, Secret

try:
    from ._version import __version__
except ImportError:
    __version__ = "0.0.0+unknown"


__all__ = [
    "__version__",
    "BaseModel",
    "BasePredictor",
    "ConcatenateIterator",
    "File",
    "Input",
    "Path",
    "Secret",
]
