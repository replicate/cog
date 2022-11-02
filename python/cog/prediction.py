from datetime import datetime
import enum
from typing import Optional, Type, Any

from pydantic import BaseModel, Field


class Status(str, enum.Enum):
    PROCESSING = "processing"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELED = "canceled"

    @staticmethod
    def is_terminal(status: "Status") -> bool:
        return status in (Status.SUCCEEDED, Status.FAILED)


class Metrics(BaseModel):
    predict_time: int


class BasePrediction(BaseModel):
    cancel_key: Optional[str] = None  # deprecated, remove when moved to `extensions`
    completed_at: Optional[datetime] = None
    created_at: Optional[datetime] = None  # TODO: should be required?
    error: Optional[str] = None
    extensions: Optional[dict] = None
    id: str
    input: Any
    logs: Optional[str] = None
    metrics: Optional[Metrics] = None
    output: Optional[Any] = None
    started_at: Optional[datetime] = None
    status: Optional[Status] = None  # TODO: should be required?
    traceparent: Optional[str] = None  # deprecated, remove when moved to `extensions`
    webhook: Optional[str] = None

    class Config:
        arbitrary_types_allowed = True


def get_prediction_type(
    InputType: Type[BaseModel], OutputType: Type[BaseModel]
) -> BasePrediction:
    class Prediction(BasePrediction):
        input: InputType  # type: ignore
        output: Optional[OutputType] = None  # type: ignore

    return Prediction  # type: ignore
