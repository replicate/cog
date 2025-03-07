import mimetypes

from pydantic import BaseModel

from .base_predictor import BasePredictor
from .mimetypes_ext import install_mime_extensions
from .server.scope import current_scope, emit_metric
from .types import (
    AsyncConcatenateIterator,
    ChatMessage,
    ConcatenateIterator,
    ExperimentalFeatureWarning,
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
    "current_scope",
    "emit_metric",
    "AsyncConcatenateIterator",
    "BaseModel",
    "BasePredictor",
    "ChatMessage",
    "ConcatenateIterator",
    "ExperimentalFeatureWarning",
    "File",
    "Input",
    "Path",
    "Secret",
]
