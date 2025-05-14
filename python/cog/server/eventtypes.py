from typing import Any, Dict, Optional, Union

from attrs import define, field, validators


# From worker parent process
#
@define
class Cancel:
    pass


@define
class PredictionInput:
    payload: Dict[str, Any]
    context: Dict[str, str] = {}


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
class PredictionMetric:
    name: str
    value: Union[float, int]


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
class Envelope:
    """
    Envelope contains an arbitrary event along with an optional tag used to
    tangle/untangle concurrent work.
    """

    event: Union[
        Cancel,
        PredictionInput,
        Shutdown,
        Log,
        PredictionMetric,
        PredictionOutput,
        PredictionOutputType,
        Done,
    ]
    tag: Optional[str] = None
