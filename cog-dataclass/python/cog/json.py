import io
import os
import pathlib
from dataclasses import asdict, is_dataclass
from datetime import datetime
from enum import Enum
from types import GeneratorType
from typing import Any, Callable

try:
    import numpy as np  # type: ignore
except ImportError:
    np = None


def make_encodeable(obj: Any) -> Any:
    """
    Returns a pickle-compatible version of the object.

    Almost JSON-compatible. Files must be done in a separate step with upload_files().
    """
    # Handle Pydantic models (v2 has model_dump(), v1 has dict())
    if hasattr(obj, "model_dump") and callable(obj.model_dump):
        return make_encodeable(obj.model_dump())
    if hasattr(obj, "dict") and callable(obj.dict):
        return make_encodeable(obj.dict())
    if is_dataclass(obj) and not isinstance(obj, type):
        return make_encodeable(asdict(obj))
    if isinstance(obj, dict):
        return {key: make_encodeable(value) for key, value in obj.items()}
    if isinstance(obj, (list, set, frozenset, GeneratorType, tuple)):
        return [make_encodeable(value) for value in obj]
    if isinstance(obj, Enum):
        return obj.value
    if isinstance(obj, datetime):
        return obj.isoformat()
    if isinstance(obj, os.PathLike):
        return pathlib.Path(obj)
    if np and not isinstance(obj, type):
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
    """
    if type(obj) == str:  # noqa: E721
        return obj
    if isinstance(obj, dict):
        return {key: upload_files(value, upload_file) for key, value in obj.items()}
    if isinstance(obj, list):
        return [upload_files(value, upload_file) for value in obj]
    if isinstance(obj, os.PathLike):
        with open(obj, "rb") as f:
            return upload_file(f)
    if isinstance(obj, io.IOBase):
        return upload_file(obj)
    return obj
