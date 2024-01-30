import os
from typing import Any, Awaitable, Callable, Set

import httpx
import structlog
from httpx._client import USER_AGENT

from ..schema import Status, WebhookEvent
from .response_throttler import ResponseThrottler
from .retry_transport import RetryTransport

log = structlog.get_logger(__name__)


def _get_version() -> str:
    try:
        try:
            from importlib.metadata import version
        except ImportError:
            pass
        else:
            return version("cog")
        import pkg_resources

        return pkg_resources.get_distribution("cog").version
    except Exception:
        return "unknown"


_user_agent = f"cog-worker/{_get_version()} {USER_AGENT}"
_response_interval = float(os.environ.get("COG_THROTTLE_RESPONSE_INTERVAL", 0.5))

# HACK: signal that we should skip the start webhook when the response interval
# is tuned below 100ms. This should help us get output sooner for models that
# are latency sensitive.
SKIP_START_EVENT = _response_interval < 0.1

WebhookSenderType = Callable[[Any, WebhookEvent], Awaitable[None]]

def filtered_caller(
    webhook: str,
    webhook_events_filter: Set[WebhookEvent],
    default_client: httpx.AsyncClient,
    retry_client: httpx.AsyncClient,
) -> WebhookSenderType:
    throttler = ResponseThrottler(response_interval=_response_interval)

    async def caller(response: Any, event: WebhookEvent) -> None:
        if event in webhook_events_filter:
            if throttler.should_send_response(response):
                if Status.is_terminal(response["status"]):
                    # For terminal updates, retry persistently
                    await retry_client.post(webhook, json=response)
                else:
                    # For other requests, don't retry, and ignore any errors
                    try:
                        await default_client.post(webhook, json=response)
                    except httpx.RequestError:
                        log.warn(
                            "caught exception while sending webhook", exc_info=True
                        )
                throttler.update_last_sent_response_time()

    return caller


def client_headers() -> dict[str, str]:
    headers = {"user-agent": _user_agent}
    auth_token = os.environ.get("WEBHOOK_AUTH_TOKEN")
    if auth_token:
        headers["authorization"] = "Bearer " + auth_token
    return headers


def httpx_client() -> httpx.AsyncClient:
    return httpx.AsyncClient(headers=client_headers())


def httpx_retry_client() -> httpx.AsyncClient:
    # This session will retry requests up to 12 times, with exponential
    # backoff. In total it'll try for up to roughly 320 seconds, providing
    # resilience through temporary networking and availability issues.
    transport = RetryTransport(
        max_attempts=12,
        backoff_factor=0.1,
        retry_status_codes=[429, 500, 502, 503, 504],
        retryable_methods=["POST"],
    )
    return httpx.AsyncClient(headers=client_headers(), transport=transport)
