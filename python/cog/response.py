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


def get_response_type(OutputType: Type[BaseModel]) -> Any:
    class Response(BaseModel):
        """The response body for a prediction"""

        status: Status = Field(...)
        output: Optional[OutputType] = None  # type: ignore
        error: Optional[str] = None

        class Config:
            arbitrary_types_allowed = True

    return Response
