import base64
import io
import mimetypes
import os
from typing import Optional

import requests


def upload_file(fh: io.IOBase, output_file_prefix: Optional[str] = None) -> str:
    if fh.seekable():
        fh.seek(0)

    if output_file_prefix is not None:
        name = getattr(fh, "name", "output")
        url = output_file_prefix + os.path.basename(name)
        resp = requests.put(url, files={"file": fh}, timeout=5)
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
