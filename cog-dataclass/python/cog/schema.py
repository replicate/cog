"""
Request/Response schema for the Cog server.

Uses dataclasses instead of pydantic. Validation is handled by _inspector.check_input().
"""

import importlib.util
import os
import os.path
import sys
from dataclasses import dataclass, field, asdict
from datetime import datetime
from enum import Enum
from types import ModuleType
from typing import Any, Dict, List, Optional, Type

BUNDLED_SCHEMA_PATH = ".cog/schema.py"


class Status(str, Enum):
    """Prediction status."""

    STARTING = "starting"
    PROCESSING = "processing"
    SUCCEEDED = "succeeded"
    CANCELED = "canceled"
    FAILED = "failed"

    @staticmethod
    def is_terminal(status: Optional["Status"]) -> bool:
        return status in {Status.SUCCEEDED, Status.CANCELED, Status.FAILED}


class WebhookEvent(str, Enum):
    """Webhook event types."""

    START = "start"
    OUTPUT = "output"
    LOGS = "logs"
    COMPLETED = "completed"

    @classmethod
    def default_events(cls) -> List["WebhookEvent"]:
        # if this is a set, it gets serialized to an array with an unstable ordering
        # so even though it's logically a set, have it as a list for deterministic schemas
        return [cls.START, cls.OUTPUT, cls.LOGS, cls.COMPLETED]


@dataclass
class PredictionRequest:
    """Request to run a prediction."""

    input: Dict[str, Any] = field(default_factory=dict)
    id: Optional[str] = None
    created_at: Optional[datetime] = None
    context: Optional[Dict[str, str]] = None
    output_file_prefix: Optional[str] = None  # deprecated
    webhook: Optional[str] = None
    webhook_events_filter: Optional[List[WebhookEvent]] = None

    def __post_init__(self) -> None:
        if self.webhook_events_filter is None:
            self.webhook_events_filter = WebhookEvent.default_events()

    def dict(self, exclude_unset: bool = False) -> Dict[str, Any]:
        """Convert to dictionary (pydantic compat)."""
        result = asdict(self)
        if exclude_unset:
            result = {k: v for k, v in result.items() if v is not None}
        return result


@dataclass
class PredictionResponse:
    """Response from a prediction."""

    input: Dict[str, Any] = field(default_factory=dict)
    output: Any = None
    id: Optional[str] = None
    version: Optional[str] = None
    created_at: Optional[datetime] = None
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None
    logs: str = ""
    error: Optional[str] = None
    status: Optional[Status] = None
    metrics: Optional[Dict[str, Any]] = None
    # Fields from request (copied but not always serialized)
    context: Optional[Dict[str, str]] = None
    output_file_prefix: Optional[str] = None
    webhook: Optional[str] = None
    webhook_events_filter: Optional[List[WebhookEvent]] = None
    # Internal: track fatal exceptions (not serialized)
    _fatal_exception: Optional[BaseException] = field(default=None, repr=False)

    def dict(self, exclude_unset: bool = False) -> Dict[str, Any]:
        """Convert to dictionary (pydantic compat)."""
        result = {
            "input": self.input,
            "output": self.output,
            "id": self.id,
            "version": self.version,
            "created_at": self.created_at,
            "started_at": self.started_at,
            "completed_at": self.completed_at,
            "logs": self.logs,
            "error": self.error,
            "status": self.status,
            "metrics": self.metrics,
        }
        if exclude_unset:
            result = {k: v for k, v in result.items() if v is not None}
        return result


@dataclass
class TrainingRequest(PredictionRequest):
    """Request to run a training job."""

    pass


@dataclass
class TrainingResponse(PredictionResponse):
    """Response from a training job."""

    pass


def create_schema_module() -> Optional[ModuleType]:
    """Load bundled schema module if it exists."""
    if not os.path.exists(BUNDLED_SCHEMA_PATH):
        return None
    name = "cog.bundled_schema"
    spec = importlib.util.spec_from_file_location(name, BUNDLED_SCHEMA_PATH)
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module
