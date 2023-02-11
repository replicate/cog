from datetime import datetime, timezone
from typing import Any, Callable, Dict, Optional

from .. import schema
from ..json import make_encodeable

ALLOWED_FIELDS_FROM_UNTRUSTED_CONTAINER = (
    # Prediction output and output metadata
    "error",
    "logs",
    "output",
    "status",
)


class PredictionMismatchError(Exception):
    pass


class PredictionTracker:
    def __init__(
        self,
        response: schema.PredictionResponse,
        webhook_caller: Optional[Callable] = None,
    ):
        self._webhook_caller = webhook_caller
        self._response = response
        self._timed_out = False

    def start(self) -> None:
        self._response.status = schema.Status.PROCESSING
        self._response.started_at = datetime.now(tz=timezone.utc)

    def is_complete(self) -> bool:
        return schema.Status.is_terminal(self._response.status)

    def timed_out(self) -> None:
        self._timed_out = True

    def update_from_webhook_payload(self, payload: schema.PredictionResponse) -> None:
        # Safety check -- only allow payloads with IDs matching self._response.
        if payload.id != self._response.id:
            raise PredictionMismatchError(
                f"received webhook payload for {payload.id} while tracking {self._response.id}"
            )

        self._update(allowed_fields(payload.dict()))

    def fail(self, message: Any) -> None:
        payload = {
            "status": schema.Status.FAILED,
            "error": message,
        }
        self._update(payload)

    def force_cancel(self) -> None:
        payload = {
            "status": schema.Status.CANCELED,
        }
        self._update(payload)

    @property
    def runtime(self) -> float:
        if self._response.started_at is None:
            return 0

        now = datetime.now(tz=timezone.utc)
        return (now - self._response.started_at).total_seconds()

    @property
    def status(self) -> Optional[schema.Status]:
        return self._response.status

    def _update(self, mapping: Dict[str, Any]) -> None:
        self._response = self._response.copy(update=mapping)
        self._adjust_cancelation_status()
        self._set_completed_at()
        self._send_webhook()

    def _adjust_cancelation_status(self) -> None:
        if not self._timed_out:
            return
        if self._response.status != schema.Status.CANCELED:
            return
        self._response.status = schema.Status.FAILED
        self._response.error = "Prediction timed out"

    def _set_completed_at(self) -> None:
        if not self._response.started_at:
            return
        if self._response.completed_at:
            return

        if schema.Status.is_terminal(self._response.status):
            self._response.completed_at = datetime.now(tz=timezone.utc)

            if self._response.status == schema.Status.SUCCEEDED:
                self._response.metrics = {
                    "predict_time": (
                        self._response.completed_at - self._response.started_at
                    ).total_seconds()
                }

    def _send_webhook(self) -> None:
        if not self._webhook_caller:
            return

        state = self._response.dict()
        payload = make_encodeable(state)
        self._webhook_caller(payload)


def allowed_fields(payload: dict) -> dict:
    return {
        k: v for k, v in payload.items() if k in ALLOWED_FIELDS_FROM_UNTRUSTED_CONTAINER
    }
