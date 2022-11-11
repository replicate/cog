import enum
from typing import Any, List, Optional, Type

from pydantic import BaseModel, Field


class Event(str, enum.Enum):
    START = "start"
    OUTPUT = "output"
    LOGS = "logs"
    COMPLETED = "completed"

    @staticmethod
    def validate(events: List[str]) -> List[str]:
        for event in events:
            assert event in {
                Event.START,
                Event.OUTPUT,
                Event.LOGS,
                Event.COMPLETED,
            }, f"Unexpected event {event}. Must be from {Event.START}, {Event.OUTPUT}, {Event.LOGS}, {Event.COMPLETED}"

        # Always include COMPLETED events
        if Event.COMPLETED not in events:
            events.append(Event.COMPLETED)

        return events

    @staticmethod
    def default_events() -> List[str]:
        return [
            Event.START,
            Event.OUTPUT,
            Event.LOGS,
            Event.COMPLETED,
        ]


class Status(str, enum.Enum):
    PROCESSING = "processing"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELED = "canceled"

    @staticmethod
    def is_terminal(status: "Status") -> bool:
        return status in {Status.SUCCEEDED, Status.CANCELED, Status.FAILED}


def get_response_type(OutputType: Type[BaseModel]) -> Any:
    class Response(BaseModel):
        """The response body for a prediction"""

        status: Status = Field(...)
        output: Optional[OutputType] = None  # type: ignore
        error: Optional[str] = None

        class Config:
            arbitrary_types_allowed = True

    return Response
