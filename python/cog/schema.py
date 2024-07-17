import importlib.util
import os
import os.path
import sys
import typing as t
from datetime import datetime
from enum import Enum
from types import ModuleType

import pydantic

BUNDLED_SCHEMA_PATH = ".cog/schema.py"


class Status(str, Enum):
    STARTING = "starting"
    PROCESSING = "processing"
    SUCCEEDED = "succeeded"
    CANCELED = "canceled"
    FAILED = "failed"

    @staticmethod
    def is_terminal(status: t.Optional["Status"]) -> bool:
        return status in {Status.SUCCEEDED, Status.CANCELED, Status.FAILED}


class WebhookEvent(str, Enum):
    START = "start"
    OUTPUT = "output"
    LOGS = "logs"
    COMPLETED = "completed"

    @classmethod
    def default_events(cls) -> t.List["WebhookEvent"]:
        # if this is a set, it gets serialized to an array with an unstable ordering
        # so even though it's logically a set, have it as a list for deterministic schemas
        # note: this change removes "uniqueItems":true
        return [cls.START, cls.OUTPUT, cls.LOGS, cls.COMPLETED]


class PredictionBaseModel(pydantic.BaseModel, extra=pydantic.Extra.allow):
    input: t.Dict[str, t.Any]


class PredictionRequest(PredictionBaseModel):
    id: t.Optional[str]
    created_at: t.Optional[datetime]

    # TODO: deprecate this
    output_file_prefix: t.Optional[str]

    webhook: t.Optional[pydantic.AnyHttpUrl]
    webhook_events_filter: t.Optional[t.List[WebhookEvent]] = (
        WebhookEvent.default_events()
    )

    @classmethod
    def with_types(cls, input_type: t.Type[t.Any]) -> t.Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__, __base__=cls, input=(t.Optional[input_type], None)
        )


class PredictionResponse(PredictionBaseModel):
    output: t.Any

    id: t.Optional[str]
    version: t.Optional[str]

    created_at: t.Optional[datetime]
    started_at: t.Optional[datetime]
    completed_at: t.Optional[datetime]

    logs: str = ""
    error: t.Optional[str]
    status: t.Optional[Status]

    metrics: t.Optional[t.Dict[str, t.Any]]

    @classmethod
    def with_types(cls, input_type: t.Type[t.Any], output_type: t.Type[t.Any]) -> t.Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__,
            __base__=cls,
            input=(t.Optional[input_type], None),
            output=(output_type, None),
        )


class TrainingRequest(PredictionRequest):
    pass


class TrainingResponse(PredictionResponse):
    pass


def create_schema_module() -> t.Optional[ModuleType]:
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
