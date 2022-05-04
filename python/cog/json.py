from enum import Enum
import io
from types import GeneratorType
from typing import Any, Callable

from pydantic import BaseModel

from .types import Path

try:
    import numpy as np  # type: ignore

    has_numpy = True
except ImportError:
    has_numpy = False


def encode_json(obj: Any, upload_file: Callable[[io.IOBase], str]) -> Any:
    """
    Returns a JSON-compatible version of the object. It will encode any Pydantic models and custom types.

    When a file is encountered, it will be passed to upload_file. Any paths will be opened and converted to files.

    Somewhat based on FastAPI's jsonable_encoder().
    """
    if isinstance(obj, BaseModel):
        return encode_json(obj.dict(exclude_unset=True), upload_file)
    if isinstance(obj, dict):
        return {key: encode_json(value, upload_file) for key, value in obj.items()}
    if isinstance(obj, (list, set, frozenset, GeneratorType, tuple)):
        return [encode_json(value, upload_file) for value in obj]
    if isinstance(obj, Enum):
        return obj.value
    if isinstance(obj, Path):
        with obj.open("rb") as f:
            return upload_file(f)
    if isinstance(obj, io.IOBase):
        return upload_file(obj)
    if has_numpy:
        if isinstance(obj, np.integer):
            return int(obj)
        if isinstance(obj, np.floating):
            return float(obj)
        if isinstance(obj, np.ndarray):
            return obj.tolist()
    return obj
