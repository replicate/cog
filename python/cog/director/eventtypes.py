from typing import Any, Callable, Dict, Optional
from enum import Enum, auto, unique

from attrs import define

from .. import schema


@unique
class Health(Enum):
    UNKNOWN = auto()
    HEALTHY = auto()
    SETUP_FAILED = auto()


@define
class Webhook:
    payload: schema.PredictionResponse


@define
class HealthcheckStatus:
    health: Health
    metadata: Optional[Dict[str, Any]] = None
