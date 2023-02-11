import typing as t
from datetime import datetime
from enum import Enum

import pydantic


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
    def default_events(cls) -> t.Set["WebhookEvent"]:
        return {cls.START, cls.OUTPUT, cls.LOGS, cls.COMPLETED}


class PredictionBaseModel(pydantic.BaseModel, extra=pydantic.Extra.allow):
    input: t.Dict[str, t.Any]


class PredictionRequest(PredictionBaseModel):
    id: t.Optional[str]
    created_at: t.Optional[datetime]

    # TODO: deprecate this
    output_file_prefix: t.Optional[str]

    webhook: t.Optional[pydantic.AnyHttpUrl]
    webhook_events_filter: t.Optional[
        t.Set[WebhookEvent]
    ] = WebhookEvent.default_events()

    @classmethod
    def with_types(cls, input_type: t.Type) -> t.Any:
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
    def with_types(cls, input_type: t.Type, output_type: t.Type) -> t.Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__,
            __base__=cls,
            input=(t.Optional[input_type], None),
            output=(output_type, None),
        )
