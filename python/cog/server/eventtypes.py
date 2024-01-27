import secrets
from typing import Any, Dict

from .. import types, schema
from attrs import define, field, validators


# From worker parent process
#
@define
class PredictionInput:
    payload: Dict[str, Any]
    id: str #= field(factory=lambda: secrets.token_hex(4))

    @classmethod
    def from_request(cls, request: schema.PredictionRequest) -> "PredictionInput":
        assert request.id, "PredictionRequest must have an id"
        payload = request.dict()["input"]
        for k, v in payload.items():
            if isinstance(v, types.URLPath):
                payload[k] = v.convert()
        return cls(payload=payload, id=request.id)


@define
class Cancel:
    id: str


@define
class Shutdown:
    pass


# From predictor child process
#
@define
class Log:
    message: str
    source: str = field(validator=validators.in_(["stdout", "stderr"]))


@define
class PredictionOutput:
    payload: Any


@define
class PredictionOutputType:
    multi: bool = False


@define
class Done:
    canceled: bool = False
    error: bool = False
    error_detail: str = ""


@define
class Heartbeat:
    pass
