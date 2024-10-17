import mimetypes

from pydantic import BaseModel

from .mimetypes_ext import install_mime_extensions
from .predictor import BasePredictor
from .server.worker import emit_metric
from .types import (
    AsyncConcatenateIterator,
    ConcatenateIterator,
    File,
    Input,
    Path,
    Secret,
)

install_mime_extensions(mimetypes)

try:
    from ._version import __version__
except ImportError:
    __version__ = "0.0.0+unknown"


__all__ = [
    "__version__",
    "BaseModel",
    "BasePredictor",
    "ConcatenateIterator",
    "AsyncConcatenateIterator",
    "File",
    "Input",
    "Path",
    "emit_metric",
    "Secret",
]
