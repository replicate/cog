import io
import mimetypes
import os
import urllib.parse
import urllib.request
from typing import Any, Dict, Iterator

import requests

FILENAME_ILLEGAL_CHARS = set("\u0000/")

# Linux allows files up to 255 bytes long. We enforce a slightly shorter
# filename so that there's room for prefixes added by
# tempfile.NamedTemporaryFile, etc.
FILENAME_MAX_LENGTH = 200


class URLFile(io.IOBase):
    """
    URLFile is a proxy object for a :class:`urllib3.response.HTTPResponse`
    object that is created lazily. It's a file-like object constructed from a
    URL that can survive pickling/unpickling.
    """

    __slots__ = ("__target__", "__url__")

    def __init__(self, url: str) -> None:
        object.__setattr__(self, "__url__", url)

    # We provide __getstate__ and __setstate__ explicitly to ensure that the
    # object is always picklable.
    def __getstate__(self) -> Dict[str, Any]:
        return {"url": object.__getattribute__(self, "__url__")}

    def __setstate__(self, state: Dict[str, Any]) -> None:
        object.__setattr__(self, "__url__", state["url"])

    # Proxy getattr/setattr/delattr through to the response object.
    def __setattr__(self, name: str, value: Any) -> None:
        if hasattr(type(self), name):
            object.__setattr__(self, name, value)
        else:
            setattr(self.__wrapped__, name, value)

    def __getattr__(self, name: str) -> Any:
        if name in ("__target__", "__wrapped__", "__url__"):
            raise AttributeError(name)
        else:
            return getattr(self.__wrapped__, name)

    def __delattr__(self, name: str) -> None:
        if hasattr(type(self), name):
            object.__delattr__(self, name)
        else:
            delattr(self.__wrapped__, name)

    # Luckily the only dunder method on HTTPResponse is __iter__
    def __iter__(self) -> Iterator[bytes]:
        return iter(self.__wrapped__)

    @property
    def __wrapped__(self) -> Any:
        try:
            return object.__getattribute__(self, "__target__")
        except AttributeError:
            url = object.__getattribute__(self, "__url__")
            resp = requests.get(url, stream=True)
            resp.raise_for_status()
            resp.raw.decode_content = True
            object.__setattr__(self, "__target__", resp.raw)
            return resp.raw

    def __repr__(self) -> str:
        try:
            target = object.__getattribute__(self, "__target__")
        except AttributeError:
            return "<{} at 0x{:x} for {!r}>".format(
                type(self).__name__, id(self), object.__getattribute__(self, "__url__")
            )
        else:
            return f"<{type(self).__name__} at 0x{id(self):x} wrapping {target!r}>"


def get_filename(url: str) -> str:
    parsed_url = urllib.parse.urlparse(url)

    if parsed_url.scheme == "data":
        resp = urllib.request.urlopen(url)  # noqa: S310
        mime_type = resp.headers.get_content_type()
        extension = mimetypes.guess_extension(mime_type)
        if extension is None:
            return "file"
        return "file" + extension

    basename = os.path.basename(parsed_url.path)
    basename = urllib.parse.unquote_plus(basename)

    # If the filename is too long, we truncate it (appending '~' to denote the
    # truncation) while preserving the file extension.
    # - truncate it
    # - append a tilde
    # - preserve the file extension
    if _len_bytes(basename) > FILENAME_MAX_LENGTH:
        basename = _truncate_filename_bytes(basename, length=FILENAME_MAX_LENGTH)

    for c in FILENAME_ILLEGAL_CHARS:
        basename = basename.replace(c, "_")

    return basename


def _len_bytes(s: str, encoding: str = "utf-8") -> int:
    return len(s.encode(encoding))


def _truncate_filename_bytes(s: str, length: int, encoding: str = "utf-8") -> str:
    """
    Truncate a filename to at most `length` bytes, preserving file extension
    and avoiding text encoding corruption from truncation.
    """
    root, ext = os.path.splitext(s.encode(encoding))
    root = root[: length - len(ext) - 1]
    return root.decode(encoding, "ignore") + "~" + ext.decode(encoding)
