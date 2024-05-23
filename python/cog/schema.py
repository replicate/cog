import secrets
from datetime import datetime
from enum import Enum
from typing import Any, Dict, List, Optional, Type

import pydantic

from .types import PYDANTIC_V2


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



class PredictionRequest(PredictionBaseModel):
    # there's a problem here where the idempotent endpoint is supposed to
    # let you pass id in the route and omit it from the input
    # however this fills in the default
    # maybe it should be allowed to be optional without the factory initially
    # and be filled in later
    #
    # actually, this changes the public api so we should really do this differently
    id: str = pydantic.Field(default_factory=lambda: secrets.token_hex(4))
    created_at: Optional[datetime] = None

    # TODO: deprecate this
    output_file_prefix: Optional[str] = None

    webhook: Optional[pydantic.AnyHttpUrl] = None
    webhook_events_filter: Optional[List[WebhookEvent]] = WebhookEvent.default_events()

    @classmethod
    def with_types(cls, input_type: Type[Any]) -> Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__, __base__=cls, input=(Optional[input_type], None)
        )


class PredictionResponse(PredictionBaseModel):
    output: Optional[Any] = None

    id: Optional[str] = None
    version: Optional[str] = None

    created_at: Optional[datetime] = None
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None

    logs: str = ""
    error: Optional[str] = None
    status: Optional[Status] = None

    metrics: Dict[str, Any] = pydantic.Field(default_factory=dict)

    @classmethod
    def with_types(cls, input_type: Type[Any], output_type: Type[Any]) -> Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__,
            __base__=cls,
            input=(Optional[input_type], None),
            output=(output_type, None),
        )


class TrainingRequest(PredictionRequest):
    pass


class TrainingResponse(PredictionResponse):
    pass
