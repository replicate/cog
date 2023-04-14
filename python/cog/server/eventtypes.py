from typing import Any, Dict

from attrs import define, field, validators


# From worker parent process
#
@define
class JobInput:
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
class JobOutput:
    payload: Any


@define
class JobOutputType:
    multi: bool = False


@define
class Done:
    canceled: bool = False
    error: bool = False
    error_detail: str = ""


@define
class Heartbeat:
    pass
