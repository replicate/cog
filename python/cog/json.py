import io
from datetime import datetime
from enum import Enum
from types import GeneratorType
from typing import Any, Callable

from pydantic import BaseModel

from .types import Path


def make_encodeable(obj: Any) -> Any:
    """
    Returns a pickle-compatible version of the object. It will encode any Pydantic models and custom types.

    It is almost JSON-compatible. Files must be done in a separate step with upload_files().

    Somewhat based on FastAPI's jsonable_encoder().
    """
    if isinstance(obj, BaseModel):
        return make_encodeable(obj.dict(exclude_unset=True))
    if isinstance(obj, dict):
        return {key: make_encodeable(value) for key, value in obj.items()}
    if isinstance(obj, (list, set, frozenset, GeneratorType, tuple)):
        return [make_encodeable(value) for value in obj]
    if isinstance(obj, Enum):
        return obj.value
    if isinstance(obj, datetime):
        return obj.isoformat()
    try:
        import numpy as np  # type: ignore

        has_numpy = True
    except ImportError:
        has_numpy = False
    if has_numpy:
        if isinstance(obj, np.integer):
            return int(obj)
        if isinstance(obj, np.floating):
            return float(obj)
        if isinstance(obj, np.ndarray):
            return obj.tolist()
    return obj


def upload_files(obj: Any, upload_file: Callable[[io.IOBase], str]) -> Any:
    """
    Iterates through an object from make_encodeable and uploads any files.

    When a file is encountered, it will be passed to upload_file. Any paths will be opened and converted to files.
    """
    if isinstance(obj, dict):
        return {key: upload_files(value, upload_file) for key, value in obj.items()}
    if isinstance(obj, list):
        return [upload_files(value, upload_file) for value in obj]
    if isinstance(obj, Path):
        with obj.open("rb") as f:
            return upload_file(f)
    if isinstance(obj, io.IOBase):
        return upload_file(obj)
    return obj
