from datetime import datetime
from enum import Enum
import typing as t

import pydantic


class Status(str, Enum):
    PROCESSING = "processing"
    SUCCEEDED = "succeeded"
    CANCELED = "canceled"
    FAILED = "failed"

    @staticmethod
    def is_terminal(status: "Status") -> bool:
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

    webhook: t.Optional[pydantic.AnyHttpUrl]
    webhook_events_filter: t.Optional[
        t.Set[WebhookEvent]
    ] = WebhookEvent.default_events()

    @classmethod
    def with_types(cls, input_type: t.Type) -> t.Any:
        return pydantic.create_model(
            cls.__name__,
            __base__=cls,
            input=(input_type, ...),
        )


class PredictionResponse(PredictionBaseModel):
    output: t.Any

    id: t.Optional[str]

    created_at: t.Optional[datetime]
    started_at: t.Optional[datetime]
    completed_at: t.Optional[datetime]

    logs: t.Optional[str]
    error: t.Optional[str]
    status: t.Optional[Status]

    @classmethod
    def with_types(cls, input_type: t.Type, output_type: t.Type) -> t.Any:
        return pydantic.create_model(
            cls.__name__,
            __base__=cls,
            input=(input_type, ...),
            output=(output_type, None),
        )
