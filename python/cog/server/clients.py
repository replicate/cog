import io
import mimetypes
import os
from typing import Any, Awaitable, Callable, Collection, Dict, Optional
from urllib.parse import urlparse

import httpx
import structlog

from ..schema import Status, WebhookEvent
from ..types import Path
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


_user_agent = f"cog-worker/{_get_version()} {httpx._client.USER_AGENT}"
_response_interval = float(os.environ.get("COG_THROTTLE_RESPONSE_INTERVAL", 0.5))

# HACK: signal that we should skip the start webhook when the response interval
# is tuned below 100ms. This should help us get output sooner for models that
# are latency sensitive.
SKIP_START_EVENT = _response_interval < 0.1

WebhookSenderType = Callable[[Any, WebhookEvent], Awaitable[None]]


def webhook_headers() -> dict[str, str]:
    headers = {"user-agent": _user_agent}
    auth_token = os.environ.get("WEBHOOK_AUTH_TOKEN")
    if auth_token:
        headers["authorization"] = "Bearer " + auth_token
    return headers


def httpx_webhook_client() -> httpx.AsyncClient:
    return httpx.AsyncClient(headers=webhook_headers(), follow_redirects=True)


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
    return httpx.AsyncClient(
        headers=webhook_headers(), transport=transport, follow_redirects=True
    )


def httpx_file_client() -> httpx.AsyncClient:
    transport = RetryTransport(
        max_attempts=3,
        backoff_factor=0.1,
        retry_status_codes=[408, 429, 500, 502, 503, 504],
        retryable_methods=["PUT"],
    )
    return httpx.AsyncClient(transport=transport, follow_redirects=True)


# I might still split this apart or inline parts of it
# I'm somewhat sympathetic to separating webhooks and files
# but they both have the same semantics of holding a client
# for the lifetime of runner
# also, both are used by PredictionEventHandler


class ClientManager:
    def __init__(self) -> None:
        # self.file_url = upload_url
        self.webhook_client = httpx_webhook_client()
        self.retry_webhook_client = httpx_retry_client()
        self.file_client = httpx_file_client()
        self.download_client = httpx.AsyncClient(follow_redirects=True)

    # webhooks

    async def send_webhook(
        self, url: str, response: Dict[str, Any], event: WebhookEvent
    ) -> None:
        if Status.is_terminal(response["status"]):
            # For terminal updates, retry persistently
            await self.retry_webhook_client.post(url, json=response)
        else:
            # For other requests, don't retry, and ignore any errors
            try:
                await self.webhook_client.post(url, json=response)
            except httpx.RequestError:
                log.warn("caught exception while sending webhook", exc_info=True)

    def make_webhook_sender(
        self, url: Optional[str], webhook_events_filter: Collection[WebhookEvent]
    ) -> WebhookSenderType:
        throttler = ResponseThrottler(response_interval=_response_interval)

        async def sender(response: Any, event: WebhookEvent) -> None:
            if url and event in webhook_events_filter:
                if throttler.should_send_response(response):
                    await self.send_webhook(url, response, event)
                    throttler.update_last_sent_response_time()

        return sender

    # files

    async def upload_file(self, fh: io.IOBase, url: str) -> str:
        """put file to signed endpoint"""
        fh.seek(0)
        # try to guess the filename of the given object
        name = getattr(fh, "name", "file")
        filename = os.path.basename(name)

        content_type, _ = mimetypes.guess_type(filename)

        # set connect timeout to slightly more than a multiple of 3 to avoid
        # aligning perfectly with TCP retransmission timer
        connect_timeout = 10
        read_timeout = 15

        # ensure trailing slash
        url_with_trailing_slash = url if url.endswith("/") else url + "/"

        resp = await self.file_client.put(
            url_with_trailing_slash + filename,
            fh,  # type: ignore
            headers={"Content-type": content_type},
            timeout=(connect_timeout, read_timeout),
        )
        resp.raise_for_status()

        # strip any signing gubbins from the URL
        final_url = urlparse(resp.url)._replace(query="").geturl()

        return final_url

    async def upload_files(self, obj: Any, url: Optional[str] = None) -> Any:
        """
        Iterates through an object from make_encodeable and uploads any files.

        When a file is encountered, it will be passed to upload_file. Any paths will be opened and converted to files.
        """
        # # it would be kind of cleaner to make the default file_url, but allow None
        # # and use that to do datauris instead
        if url is None:
            return obj
        if isinstance(obj, dict):
            return {
                key: await self.upload_files(value, url) for key, value in obj.items()
            }
        if isinstance(obj, list):
            return [await self.upload_files(value, url) for value in obj]
        if isinstance(obj, Path):
            with obj.open("rb") as f:
                return await self.upload_file(f, url)
        if isinstance(obj, io.IOBase):
            return await self.upload_file(obj, url)
        return obj
