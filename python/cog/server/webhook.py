import os
from typing import Any, Callable, Set

import requests
import structlog
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry  # type: ignore

from ..schema import Status, WebhookEvent
from .response_throttler import ResponseThrottler

log = structlog.get_logger(__name__)


def _get_version() -> str:
    use_importlib = True
    try:
        from importlib.metadata import version
    except ImportError:
        use_importlib = False

    try:
        if use_importlib:
            return version("cog")
        import pkg_resources

        return pkg_resources.get_distribution("cog").version
    except Exception:
        return "unknown"


_user_agent = f"cog-worker/{_get_version()}"
_response_interval = float(os.environ.get("COG_THROTTLE_RESPONSE_INTERVAL", 0.5))


def webhook_caller_filtered(
    webhook: str, webhook_events_filter: Set[WebhookEvent]
) -> Callable:
    upstream_caller = webhook_caller(webhook)

    def caller(response: Any, event: WebhookEvent) -> None:
        if event in webhook_events_filter:
            upstream_caller(response)

    return caller


def webhook_caller(webhook: str) -> Callable:
    # TODO: we probably don't need to create new sessions and new throttlers
    # for every prediction.
    throttler = ResponseThrottler(response_interval=_response_interval)

    default_session = requests_session()
    retry_session = requests_session_with_retries()

    def caller(response: Any) -> None:
        if throttler.should_send_response(response):
            if Status.is_terminal(response["status"]):
                # For terminal updates, retry persistently
                retry_session.post(webhook, json=response)
            else:
                # For other requests, don't retry, and ignore any errors
                try:
                    default_session.post(webhook, json=response)
                except requests.exceptions.RequestException:
                    log.warn("caught exception while sending webhook", exc_info=True)
            throttler.update_last_sent_response_time()

    return caller


def requests_session() -> requests.Session:
    session = requests.Session()
    session.headers["user-agent"] = (
        _user_agent + " " + str(session.headers["user-agent"])
    )
    auth_token = os.environ.get("WEBHOOK_AUTH_TOKEN")
    if auth_token:
        session.headers["authorization"] = "Bearer " + auth_token

    return session


def requests_session_with_retries() -> requests.Session:
    # This session will retry requests up to 12 times, with exponential
    # backoff. In total it'll try for up to roughly 320 seconds, providing
    # resilience through temporary networking and availability issues.
    session = requests_session()
    adapter = HTTPAdapter(
        max_retries=Retry(
            total=12,
            backoff_factor=0.1,
            status_forcelist=[429, 500, 502, 503, 504],
            allowed_methods=["POST"],
        )
    )
    session.mount("http://", adapter)
    session.mount("https://", adapter)

    return session
