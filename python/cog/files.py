import base64
import io
import mimetypes
import os
from typing import Any, Callable
from urllib.parse import urlparse

import httpx
import requests

from .json import upload_files
from .server.retry_transport import RetryTransport


async def upload_file(fh: io.IOBase, output_file_prefix: str = None) -> str:
    fh.seek(0)

    if output_file_prefix is not None:
        name = getattr(fh, "name", "output")
        url = output_file_prefix + os.path.basename(name)
        resp = requests.put(url, files={"file": fh})
        resp.raise_for_status()
        return url

    b = fh.read()
    # The file handle is strings, not bytes
    if isinstance(b, str):
        b = b.encode("utf-8")
    encoded_body = base64.b64encode(b)
    if getattr(fh, "name", None):
        # despite doing a getattr check here, pyright complains that io.IOBase has no attribute name
        # TODO: switch to typing.IO[]?
        mime_type = mimetypes.guess_type(fh.name)[0]  # type: ignore
    else:
        mime_type = "application/octet-stream"
    s = encoded_body.decode("utf-8")
    return f"data:{mime_type};base64,{s}"


def httpx_file_client() -> httpx.AsyncClient:
    transport = RetryTransport(
        max_attempts=3,
        backoff_factor=0.1,
        retry_status_codes=[408, 429, 500, 502, 503, 504],
        retryable_methods=["PUT"],
    )
    return httpx.AsyncClient(transport=transport)


def make_file_uploader(client: httpx.AsyncClient, url: str) -> Callable[[Any], Any]:
    async def file_uploader(output: Any) -> Any:
        async def upload_file(fh: io.IOBase) -> str:
            # put file to signed endpoint
            fh.seek(0)
            # guess filename
            name = getattr(fh, "name", "file")
            filename = os.path.basename(name)
            content_type, _ = mimetypes.guess_type(filename)

            # set connect timeout to slightly more than a multiple of 3 to avoid
            # aligning perfectly with TCP retransmission timer
            connect_timeout = 10
            read_timeout = 15

            # ensure trailing slash
            url_with_trailing_slash = url if url.endswith("/") else url + "/"

            resp = await client.put(
                url_with_trailing_slash + filename,
                fh,  # type: ignore
                headers={"Content-type": content_type},
                timeout=(connect_timeout, read_timeout),
            )
            resp.raise_for_status()

            # strip any signing gubbins from the URL
            final_url = urlparse(resp.url)._replace(query="").geturl()

            return final_url

        return await upload_files(output, upload_file=upload_file)

    return file_uploader
