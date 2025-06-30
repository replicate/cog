import io
import os
import pathlib
from datetime import datetime
from enum import Enum
from types import GeneratorType
from typing import Any, Callable

from pydantic import BaseModel

# numpy is an optional dependency, but the process of importing it is not
# thread-safe, so we attempt the import once here.
try:
    import numpy as np  # type: ignore
except ImportError:
    np = None

from .types import PYDANTIC_V2


def make_encodeable(obj: Any) -> Any:  # pylint: disable=too-many-return-statements
    """
    Returns a pickle-compatible version of the object. It will encode any Pydantic models and custom types.

    It is almost JSON-compatible. Files must be done in a separate step with upload_files().

    Somewhat based on FastAPI's jsonable_encoder().
    """

    if isinstance(obj, BaseModel):
        if PYDANTIC_V2:
            return make_encodeable(obj.model_dump(exclude_unset=True))
        else:
            return make_encodeable(obj.dict())
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
    if np:
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
    # skip four isinstance checks for fast text models
    if type(obj) == str:  # noqa: E721 # pylint: disable=unidiomatic-typecheck
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
