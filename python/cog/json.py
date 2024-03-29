from datetime import datetime
from enum import Enum
from types import GeneratorType
from typing import Any

from pydantic import BaseModel


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
    except ImportError:
        pass
    else:
        if isinstance(obj, np.integer):
            return int(obj)
        if isinstance(obj, np.floating):
            return float(obj)
        if isinstance(obj, np.ndarray):
            return obj.tolist()
    return obj
