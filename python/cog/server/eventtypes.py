from typing import Any, Dict

from attrs import define, field, validators


# From worker parent process
#
@define
class PredictionInput:
    payload: Dict[str, Any]


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
