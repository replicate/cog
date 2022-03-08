import enum
from typing import Any

from pydantic import BaseModel, Field


class Status(str, enum.Enum):
    PROCESSING = "processing"
    SUCCESS = "success"
    FAILED = "failed"  # FIXME: "failure"?


def get_response_type(OutputType: Any):
    class Response(BaseModel):
        """The status of a prediction."""

        status: Status = Field(...)
        output: OutputType = None
        error: str = None

        class Config:
            arbitrary_types_allowed = True

    return Response
