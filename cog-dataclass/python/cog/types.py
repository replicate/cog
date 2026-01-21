"""
Cog SDK type definitions.

This module provides core types for defining predictor inputs and outputs:
- Path: File path type that supports URL inputs
- Secret: Secure string type that masks its value
- File: Deprecated file type (use Path instead)
- ConcatenateIterator: Streaming output iterator
- AsyncConcatenateIterator: Async streaming output iterator
"""

import io
import mimetypes
import os
import pathlib
import shutil
import tempfile
import urllib.parse
import urllib.request
from abc import abstractmethod
from dataclasses import dataclass
from typing import (
    Any,
    AsyncIterator,
    Dict,
    Iterator,
    Optional,
    Type,
    TypeVar,
)

import requests


# Constants for filename handling
FILENAME_ILLEGAL_CHARS = set("\u0000/")
FILENAME_MAX_LENGTH = 200


def _len_bytes(s: str) -> int:
    """Return the length of a string in bytes (UTF-8)."""
    return len(s.encode("utf-8"))


def _truncate_filename_bytes(filename: str, length: int) -> str:
    """Truncate a filename to a maximum byte length, preserving extension."""
    if _len_bytes(filename) <= length:
        return filename

    # Split filename and extension
    name, ext = os.path.splitext(filename)

    # Reserve space for tilde and extension
    max_name_length = length - _len_bytes(ext) - 1

    # Truncate name
    encoded = name.encode("utf-8")
    truncated = encoded[:max_name_length].decode("utf-8", errors="ignore")

    return f"{truncated}~{ext}"


def get_filename(url: str) -> str:
    """Extract a filename from a URL."""
    parsed_url = urllib.parse.urlparse(url)

    if parsed_url.scheme == "data":
        # Safe: scheme is validated to be 'data:' before urlopen
        with urllib.request.urlopen(url) as resp:  # noqa: S310
            mime_type = resp.headers.get_content_type()
            extension = mimetypes.guess_extension(mime_type)
            if extension is None:
                return "file"
            return "file" + extension

    basename = os.path.basename(parsed_url.path)
    basename = urllib.parse.unquote_plus(basename)

    # Truncate if too long
    if _len_bytes(basename) > FILENAME_MAX_LENGTH:
        basename = _truncate_filename_bytes(basename, length=FILENAME_MAX_LENGTH)

    # Replace illegal characters
    for c in FILENAME_ILLEGAL_CHARS:
        basename = basename.replace(c, "_")

    return basename


########################################
# Secret
########################################


@dataclass(frozen=True)
class Secret:
    """
    A secret string value that masks itself in string representations.

    Use this type for sensitive data like API keys or passwords that should
    not be logged or displayed.

    Example:
        def predict(self, api_key: Secret) -> str:
            key = api_key.get_secret_value()
            # Use key...
    """

    secret_value: Optional[str] = None

    def __repr__(self) -> str:
        return f"Secret({str(self)})"

    def __str__(self) -> str:
        return "**********" if self.secret_value is not None else ""

    def get_secret_value(self) -> Optional[str]:
        """Return the actual secret value."""
        return self.secret_value


########################################
# URLFile
########################################


class URLFile(io.IOBase):
    """
    URLFile is a proxy object for a :class:`urllib3.response.HTTPResponse`
    object that is created lazily. It's a file-like object constructed from a
    URL that can survive pickling/unpickling.
    """

    __slots__ = ("__target__", "__url__", "name")

    def __init__(self, url: str, filename: Optional[str] = None) -> None:
        parsed = urllib.parse.urlparse(url)
        if parsed.scheme not in {"http", "https"}:
            raise ValueError(
                "URLFile requires URL to conform to HTTP or HTTPS protocol"
            )

        if not filename:
            filename = os.path.basename(parsed.path)

        object.__setattr__(self, "name", filename)
        object.__setattr__(self, "__url__", url)

    def __del__(self) -> None:
        try:
            object.__getattribute__(self, "__target__")
        except AttributeError:
            # Do nothing when tearing down the object if the response object
            # hasn't been created yet.
            return

        super().__del__()

    # We provide __getstate__ and __setstate__ explicitly to ensure that the
    # object is always picklable.
    def __getstate__(self) -> Dict[str, Any]:
        return {
            "name": object.__getattribute__(self, "name"),
            "url": object.__getattribute__(self, "__url__"),
        }

    def __setstate__(self, state: Dict[str, Any]) -> None:
        object.__setattr__(self, "name", state["name"])
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
        elif name == "name":
            return object.__getattribute__(self, "name")
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
            pass
        url = object.__getattribute__(self, "__url__")
        resp = requests.get(url, stream=True, timeout=10)
        resp.raise_for_status()
        resp.raw.decode_content = True
        object.__setattr__(self, "__target__", resp.raw)
        return resp.raw

    def __repr__(self) -> str:
        try:
            target = object.__getattribute__(self, "__target__")
        except AttributeError:
            return f"<{type(self).__name__} at 0x{id(self):x} for {object.__getattribute__(self, '__url__')!r}>"

        return f"<{type(self).__name__} at 0x{id(self):x} wrapping {target!r}>"


########################################
# File (Deprecated)
########################################


class File(io.IOBase):
    """
    Deprecated: use Path instead.

    A file-like object that can be constructed from a URL or data URI.
    """

    @classmethod
    def validate(cls, value: Any) -> io.IOBase:
        """Validate and convert a value to a file-like object."""
        if isinstance(value, io.IOBase):
            return value

        parsed_url = urllib.parse.urlparse(value)
        if parsed_url.scheme == "data":
            # Safe: scheme is validated to be 'data:' before urlopen
            with urllib.request.urlopen(value) as res:  # noqa: S310
                return io.BytesIO(res.read())
        if parsed_url.scheme in ("http", "https"):
            return URLFile(value)
        raise ValueError(
            f"'{parsed_url.scheme}' is not a valid URL scheme. "
            "'data', 'http', or 'https' is supported."
        )


########################################
# URLPath
########################################


class URLPath(pathlib.PosixPath):
    """
    URLPath is a nasty hack to ensure that we can defer the downloading of a
    URL passed as a path until later in prediction dispatch.

    It subclasses pathlib.PosixPath only so that it can pass isinstance(_,
    pathlib.Path) checks.
    """

    _path: Optional["Path"]

    # pylint: disable=super-init-not-called
    def __init__(self, *, source: str, filename: str, fileobj: io.IOBase) -> None:
        if len(filename) > FILENAME_MAX_LENGTH:
            filename = _truncate_filename_bytes(filename, FILENAME_MAX_LENGTH)

        self.source = source
        self.filename = filename
        self.fileobj = fileobj

        self._path = None

    def __new__(cls, *, source: str, filename: str, fileobj: io.IOBase) -> "URLPath":
        # PosixPath.__new__ requires path segments, but we don't have a real path
        # Use a placeholder that will be replaced
        obj = super().__new__(cls, filename)
        return obj

    def convert(self) -> "Path":
        """Download the URL content to a temporary file and return its Path."""
        if self._path is None:
            # pylint: disable=consider-using-with
            dest = tempfile.NamedTemporaryFile(suffix=self.filename, delete=False)
            shutil.copyfileobj(self.fileobj, dest)
            dest.close()
            self._path = Path(dest.name)
        return self._path

    def unlink(self, missing_ok: bool = False) -> None:
        """Remove the temporary file if it exists."""
        if self._path:
            self._path.unlink(missing_ok=missing_ok)

    def __str__(self) -> str:
        # FastAPI's jsonable_encoder will encode subclasses of pathlib.Path by
        # calling str() on them
        return self.source


########################################
# Path
########################################


class Path(pathlib.PosixPath):
    """
    A path type that can be constructed from URLs.

    When a URL is passed, it creates a URLPath that defers downloading
    until the file is actually needed.

    Example:
        def predict(self, image: Path) -> Path:
            # image could be a local path or downloaded from URL
            return process(image)
    """

    @classmethod
    def validate(cls, value: Any) -> pathlib.Path:
        """Validate and convert a value to a Path."""
        if isinstance(value, pathlib.Path):
            return value

        return URLPath(
            source=value,
            filename=get_filename(value),
            fileobj=File.validate(value),
        )

    # Pydantic v2 support - allows using cog.Path in pydantic.BaseModel
    @classmethod
    def __get_pydantic_core_schema__(
        cls,
        source: Type[Any],
        handler: Any,
    ) -> Any:
        from pydantic_core import core_schema  # pylint: disable=import-outside-toplevel

        return core_schema.union_schema(
            [
                core_schema.is_instance_schema(pathlib.Path),
                core_schema.no_info_plain_validator_function(cls.validate),
            ]
        )

    @classmethod
    def __get_pydantic_json_schema__(
        cls, core_schema: Any, handler: Any
    ) -> Dict[str, Any]:
        json_schema = handler(core_schema)
        json_schema.update(type="string", format="uri")
        return json_schema


########################################
# Iterators
########################################

Item = TypeVar("Item")


class ConcatenateIterator(Iterator[Item]):
    """
    An iterator that yields items which should be concatenated for display.

    Use this as a return type hint for streaming text output where the
    individual chunks should be joined together.

    Example:
        def predict(self, prompt: str) -> ConcatenateIterator[str]:
            for token in generate_tokens(prompt):
                yield token
    """

    @abstractmethod
    def __next__(self) -> Item: ...


class AsyncConcatenateIterator(AsyncIterator[Item]):
    """
    An async iterator that yields items which should be concatenated for display.

    Use this as a return type hint for async streaming text output where the
    individual chunks should be joined together.

    Example:
        async def predict(self, prompt: str) -> AsyncConcatenateIterator[str]:
            async for token in generate_tokens_async(prompt):
                yield token
    """

    @abstractmethod
    async def __anext__(self) -> Item: ...


########################################
# Warnings
########################################


class ExperimentalFeatureWarning(UserWarning):
    """Warning for experimental features that may change or be removed."""

    pass
