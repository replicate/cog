"""
Event types for worker communication.
"""

from dataclasses import dataclass, field
from typing import Any, Dict, Optional, Union


# From worker parent process


@dataclass
class Cancel:
    pass


@dataclass
class PredictionInput:
    payload: Dict[str, Any]
    context: Dict[str, str] = field(default_factory=dict)


@dataclass
class Shutdown:
    pass


@dataclass
class Healthcheck:
    pass


# From predictor child process


@dataclass
class Log:
    message: str
    source: str = "stdout"

    def __post_init__(self) -> None:
        if self.source not in ("stdout", "stderr"):
            raise ValueError(
                f"source must be 'stdout' or 'stderr', got {self.source!r}"
            )


@dataclass
class PredictionMetric:
    name: str
    value: Union[float, int]


@dataclass
class PredictionOutput:
    payload: Any


@dataclass
class PredictionOutputType:
    multi: bool = False


@dataclass
class Done:
    canceled: bool = False
    error: bool = False
    error_detail: str = ""
    event_type: str = "prediction"  # "prediction", "setup", or "healthcheck"


@dataclass
class Envelope:
    """
    Envelope contains an arbitrary event along with an optional tag used to
    tangle/untangle concurrent work.
    """

    event: Union[
        Cancel,
        Healthcheck,
        PredictionInput,
        Shutdown,
        Log,
        PredictionMetric,
        PredictionOutput,
        PredictionOutputType,
        Done,
    ]
    tag: Optional[str] = None
