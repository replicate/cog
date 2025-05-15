import importlib.util
import os
import os.path
import sys
from datetime import datetime
from enum import Enum
from types import ModuleType
from typing import Any, Dict, List, Optional, Type

import pydantic

from .types import PYDANTIC_V2

BUNDLED_SCHEMA_PATH = ".cog/schema.py"


class Status(str, Enum):
    STARTING = "starting"
    PROCESSING = "processing"
    SUCCEEDED = "succeeded"
    CANCELED = "canceled"
    FAILED = "failed"

    @staticmethod
    def is_terminal(status: Optional["Status"]) -> bool:
        return status in {Status.SUCCEEDED, Status.CANCELED, Status.FAILED}


class WebhookEvent(str, Enum):
    START = "start"
    OUTPUT = "output"
    LOGS = "logs"
    COMPLETED = "completed"

    @classmethod
    def default_events(cls) -> List["WebhookEvent"]:
        # if this is a set, it gets serialized to an array with an unstable ordering
        # so even though it's logically a set, have it as a list for deterministic schemas
        # note: this change removes "uniqueItems":true
        return [cls.START, cls.OUTPUT, cls.LOGS, cls.COMPLETED]


class PredictionBaseModel(pydantic.BaseModel):
    input: Dict[str, Any]

    if PYDANTIC_V2:
        model_config = pydantic.ConfigDict(use_enum_values=True)  # type: ignore
    else:

        class Config:
            # When using `choices`, the type is converted into an enum to validate
            # But, after validation, we want to pass the actual value to predict(), not the enum object
            use_enum_values = True


if PYDANTIC_V2:
    from pydantic.networks import UrlConstraints
    from pydantic_core import Url
    from typing_extensions import Annotated  # added to typing in python 3.9

    WebhookUrl = Annotated[
        Url, UrlConstraints(allowed_schemes=["http", "https"], max_length=65536)
    ]
else:
    WebhookUrl = pydantic.AnyUrl


class PredictionRequest(PredictionBaseModel):
    id: Optional[str] = None
    created_at: Optional[datetime] = None
    context: Optional[Dict[str, str]] = None

    # TODO: deprecate this
    output_file_prefix: Optional[str] = None

    webhook: Optional[WebhookUrl] = None
    webhook_events_filter: Optional[List[WebhookEvent]] = pydantic.Field(
        default=WebhookEvent.default_events(),
    )

    @classmethod
    def with_types(cls, input_type: Type[Any]) -> Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__, __base__=cls, input=(Optional[input_type], None)
        )


class PredictionResponse(PredictionBaseModel):
    output: Any = None

    id: Optional[str] = None
    version: Optional[str] = None

    created_at: Optional[datetime] = None
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None

    logs: str = ""
    error: Optional[str] = None
    status: Optional[Status] = None

    metrics: Optional[Dict[str, Any]] = None

    # This is used to track a fatal exception that occurs during a prediction.
    # "Fatal" means that we require the worker to be shut down to recover:
    # regular exceptions raised during predict are handled and do not use this
    # field.
    _fatal_exception: Optional[BaseException] = pydantic.PrivateAttr(default=None)

    @classmethod
    def with_types(cls, input_type: Type[Any], output_type: Type[Any]) -> Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__,
            __base__=cls,
            input=(Optional[input_type], None),
            output=(Optional[output_type], None),
        )


class TrainingRequest(PredictionRequest):
    pass


class TrainingResponse(PredictionResponse):
    pass


def create_schema_module() -> Optional[ModuleType]:
    if not os.path.exists(BUNDLED_SCHEMA_PATH):
        return None
    name = "cog.bundled_schema"
    spec = importlib.util.spec_from_file_location(name, BUNDLED_SCHEMA_PATH)
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module
