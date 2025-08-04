import os
from concurrent.futures import ThreadPoolExecutor
from typing import Any, Callable, Set

import requests
import structlog
from fastapi.encoders import jsonable_encoder
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

from ..schema import PredictionResponse, Status, WebhookEvent
from ..types import PYDANTIC_V2
from .response_throttler import ResponseThrottler
from .telemetry import current_trace_context
from .useragent import get_user_agent

log = structlog.get_logger(__name__)

_response_interval = float(os.environ.get("COG_THROTTLE_RESPONSE_INTERVAL", 0.5))
_webhook_timeout = float(
    os.environ.get("COG_WEBHOOK_TIMEOUT", 10.0)
)  # 10 second timeout by default
_webhook_executor = ThreadPoolExecutor(max_workers=4, thread_name_prefix="webhook")

# HACK: signal that we should skip the start webhook when the response interval
# is tuned below 100ms. This should help us get output sooner for models that
# are latency sensitive.
SKIP_START_EVENT = _response_interval < 0.1


def webhook_caller_filtered(
    webhook: str,
    webhook_events_filter: Set[WebhookEvent],
) -> Callable[[Any, WebhookEvent], None]:
    # Create a session for this webhook
    default_session = requests_session()
    retry_session = requests_session_with_retries()
    throttler = ResponseThrottler(response_interval=_response_interval)

    def _send_webhook(response: PredictionResponse, session: requests.Session) -> None:
        if PYDANTIC_V2:
            dict_response = jsonable_encoder(response.model_dump(exclude_unset=True))
        else:
            dict_response = jsonable_encoder(response.dict(exclude_unset=True))

        try:
            session.post(webhook, json=dict_response, timeout=_webhook_timeout)
        except requests.exceptions.Timeout:
            log.warn("webhook request timed out", webhook=webhook)
        except requests.exceptions.RequestException:
            log.warn("caught exception while sending webhook", exc_info=True)

    def caller(response: PredictionResponse, event: WebhookEvent) -> None:
        if event not in webhook_events_filter:
            return

        if not throttler.should_send_response(response):
            return

        # Use a separate thread for webhook calls to avoid blocking
        if Status.is_terminal(response.status):
            # For terminal updates, retry persistently but in background
            _webhook_executor.submit(_send_webhook, response, retry_session)
        else:
            # For other requests, don't retry, and ignore any errors
            _webhook_executor.submit(_send_webhook, response, default_session)

        throttler.update_last_sent_response_time()

    return caller


def webhook_caller(webhook: str) -> Callable[[Any], None]:
    # TODO: we probably don't need to create new sessions and new throttlers
    # for every prediction.
    throttler = ResponseThrottler(response_interval=_response_interval)

    default_session = requests_session()
    retry_session = requests_session_with_retries()

    def _send_webhook(response: PredictionResponse, session: requests.Session) -> None:
        if PYDANTIC_V2:
            dict_response = jsonable_encoder(response.model_dump(exclude_unset=True))
        else:
            dict_response = jsonable_encoder(response.dict(exclude_unset=True))

        try:
            session.post(webhook, json=dict_response, timeout=_webhook_timeout)
        except requests.exceptions.Timeout:
            log.warn("webhook request timed out", webhook=webhook)
        except requests.exceptions.RequestException:
            log.warn("caught exception while sending webhook", exc_info=True)

    def caller(response: PredictionResponse) -> None:
        if not throttler.should_send_response(response):
            return

        # Use a separate thread for webhook calls to avoid blocking
        if Status.is_terminal(response.status):
            # For terminal updates, retry persistently but in background
            _webhook_executor.submit(_send_webhook, response, retry_session)
        else:
            # For other requests, don't retry, and ignore any errors
            _webhook_executor.submit(_send_webhook, response, default_session)

        throttler.update_last_sent_response_time()

    return caller


def requests_session() -> requests.Session:
    session = requests.Session()
    session.headers["user-agent"] = (
        get_user_agent() + " " + str(session.headers["user-agent"])
    )

    ctx = current_trace_context() or {}
    for key, value in ctx.items():
        session.headers[key] = str(value)

    auth_token = os.environ.get("WEBHOOK_AUTH_TOKEN")
    if auth_token:
        session.headers["authorization"] = "Bearer " + auth_token

    return session


def requests_session_with_retries() -> requests.Session:
    # This session will retry requests up to 6 times (reduced from 12), with exponential
    # backoff. In total it'll try for up to roughly 60 seconds (reduced from 320s).
    session = requests_session()
    adapter = HTTPAdapter(
        max_retries=Retry(
            total=6,  # Reduced from 12 to avoid blocking too long
            backoff_factor=0.1,
            status_forcelist=[429, 500, 502, 503, 504],
            allowed_methods=["POST"],
        )
    )
    session.mount("http://", adapter)
    session.mount("https://", adapter)

    return session
