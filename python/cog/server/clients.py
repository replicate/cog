import base64
import io
import mimetypes
import os
from typing import Any, AsyncIterator, Awaitable, Callable, Collection, Dict, Optional
from urllib.parse import urlparse

import httpx
import structlog

from .. import types
from ..schema import Status, WebhookEvent
from ..types import Path
from .eventtypes import PredictionInput
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
    # set connect timeout to slightly more than a multiple of 3 to avoid
    # aligning perfectly with TCP retransmission timer
    # requests has no write timeout, keep that
    # httpx default for pool is 5, use that
    timeout = httpx.Timeout(connect=10, read=15, write=None, pool=5)
    return httpx.AsyncClient(
        transport=transport,
        follow_redirects=True,
        timeout=timeout,
        verify=os.environ["CURL_CA_BUNDLE"],
    )


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

    async def aclose(self) -> None:
        await self.webhook_client.aclose()
        await self.retry_webhook_client.aclose()
        await self.file_client.aclose()
        await self.download_client.aclose()

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

    async def upload_file(self, fh: io.IOBase, url: Optional[str]) -> str:
        """put file to signed endpoint"""
        fh.seek(0)
        # try to guess the filename of the given object
        name = getattr(fh, "name", "file")
        filename = os.path.basename(name)

        guess, _ = mimetypes.guess_type(filename)
        content_type = guess or "application/octet-stream"

        # this code path happens when running outside replicate without upload-url
        # in that case we need to return data uris
        if url is None:
            return file_to_data_uri(fh, content_type)
        assert url

        # ensure trailing slash
        url_with_trailing_slash = url if url.endswith("/") else url + "/"

        async def chunk_file_reader() -> AsyncIterator[bytes]:
            while 1:
                chunk = fh.read(1024 * 1024)
                if isinstance(chunk, str):
                    chunk = chunk.encode("utf-8")
                if not chunk:
                    break
                yield chunk

        url = url_with_trailing_slash + filename
        if url and "internal" in url:
            resp1 = await self.file_client.put(
                url,
                content=b"",
                headers={"Content-Type": content_type},
                follow_redirects=False,
            )
            if resp1.status_code == 307:
                url = resp1.headers["Location"]
        resp = await self.file_client.put(
            url,
            content=chunk_file_reader(),
            headers={"Content-Type": content_type},
        )
        resp.raise_for_status()

        # strip any signing gubbins from the URL
        final_url = urlparse(str(resp.url))._replace(query="").geturl()

        return final_url

    # this previously lived in json.upload_files, but it's clearer here
    async def upload_files(self, obj: Any, url: Optional[str]) -> Any:
        """
        Iterates through an object from make_encodeable and uploads any files.

        When a file is encountered, it will be passed to upload_file. Any paths will be opened and converted to files.
        """
        # # it would be kind of cleaner to make the default file_url
        # # instead of skipping entirely, we need to convert to datauri
        # if url is None:
        #     return obj
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

    # inputs

    # currently we only handle lists, so flattening each value would be sufficient
    # but it would be preferable to support dicts and other collections

    async def convert_prediction_input(self, prediction_input: PredictionInput) -> None:
        # this sucks lol
        # FIXME: handle e.g. dict[str, list[Path]]
        # FIXME: download files concurrently
        for k, v in prediction_input.payload.items():
            if isinstance(v, types.DataURLTempFilePath):
                prediction_input.payload[k] = v.convert()
            if isinstance(v, types.URLTempFile):
                real_path = await v.convert(self.download_client)
                prediction_input.payload[k] = real_path


def file_to_data_uri(fh: io.IOBase, mime_type: str) -> str:
    b = fh.read()
    # The file handle is strings, not bytes
    # this can happen if we're "uploading" StringIO
    if isinstance(b, str):
        b = b.encode("utf-8")
    encoded_body = base64.b64encode(b)
    s = encoded_body.decode("utf-8")
    return f"data:{mime_type};base64,{s}"
