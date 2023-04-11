import base64
import io
import mimetypes
import os
from urllib.parse import urlparse

import requests


def upload_file(fh: io.IOBase, output_file_prefix: str = None) -> str:
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
        # despite doing a getattr check here, mypy complains that io.IOBase has no attribute name
        mime_type = mimetypes.guess_type(fh.name)[0]  # type: ignore
    else:
        mime_type = "application/octet-stream"
    s = encoded_body.decode("utf-8")
    return f"data:{mime_type};base64,{s}"


def guess_filename(obj: io.IOBase) -> str:
    """Tries to guess the filename of the given object."""
    name = getattr(obj, "name", "file")
    return os.path.basename(name)


def put_file_to_signed_endpoint(
    fh: io.IOBase, endpoint: str, client: requests.Session
) -> str:
    fh.seek(0)

    filename = guess_filename(fh)
    content_type, _ = mimetypes.guess_type(filename)

    # set connect timeout to slightly more than a multiple of 3 to avoid
    # aligning perfectly with TCP retransmission timer
    connect_timeout = 10
    read_timeout = 15

    resp = client.put(
        ensure_trailing_slash(endpoint) + filename,
        fh,  # type: ignore
        headers={"Content-type": content_type},
        timeout=(connect_timeout, read_timeout),
    )
    resp.raise_for_status()

    # strip any signing gubbins from the URL
    final_url = urlparse(resp.url)._replace(query="").geturl()

    return final_url


def ensure_trailing_slash(url: str) -> str:
    """
    Adds a trailing slash to `url` if not already present, and then returns it.
    """
    if url.endswith("/"):
        return url
    else:
        return url + "/"
