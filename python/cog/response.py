from typing import Any

from pydantic import BaseModel, Field

from .json import JSON_ENCODERS


def get_response_type(OutputType: Any):
    class CogResponse(BaseModel):
        status: str = Field(...)
        output: OutputType = Field(...)

        class Config:
            arbitrary_types_allowed = True
            json_encoders = JSON_ENCODERS

    return CogResponse
