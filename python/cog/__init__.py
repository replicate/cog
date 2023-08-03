import time
import sys


def logtime(msg: str) -> None:
    print(f"===TIME {time.time():.4f} {msg}===", file=sys.stderr)


logtime("cog/__init__.py")
# never mind all this, predict.py will just have to from cog.predictor import BasePredictor

# from pydantic import BaseModel

# from .predictor import BasePredictor
# from .types import ConcatenateIterator, File, Input, Path


try:
    from ._version import __version__
except ImportError:
    __version__ = "0.0.0+unknown"


__all__ = [
    "__version__",
    # "BaseModel",
    # "BasePredictor",
    # "ConcatenateIterator",
    # "File",
    # "Input",
    # "Path",
]

