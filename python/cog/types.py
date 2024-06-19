import io
import mimetypes
import os
import pathlib
import shutil
import tempfile
import urllib.parse
import urllib.request
import urllib.response
from typing import Any, AsyncIterator, Dict, Iterator, List, Optional, TypeVar, Union

import httpx
import requests
from pydantic import Field, SecretStr

FILENAME_ILLEGAL_CHARS = set("\u0000/")

# Linux allows files up to 255 bytes long. We enforce a slightly shorter
# filename so that there's room for prefixes added by
# tempfile.NamedTemporaryFile, etc.
FILENAME_MAX_LENGTH = 200


def Input(
    default: Any = ...,
    description: str = None,
    ge: float = None,
    le: float = None,
    min_length: int = None,
    max_length: int = None,
    regex: str = None,
    choices: List[Union[str, int]] = None,
) -> Any:
    """Input is similar to pydantic.Field, but doesn't require a default value to be the first argument."""
    return Field(
        default,
        description=description,
        ge=ge,
        le=le,
        min_length=min_length,
        max_length=max_length,
        regex=regex,
        choices=choices,
    )


class Secret(SecretStr):
    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        field_schema.update(
            {
                "type": "string",
                "format": "password",
                "x-cog-secret": True,
            }
        )


class File(io.IOBase):
    """Deprecated: use Path instead."""

    validate_always = True

    @classmethod
    def __get_validators__(cls) -> Iterator[Any]:
        yield cls.validate

    @classmethod
    def validate(cls, value: Any) -> io.IOBase:
        if isinstance(value, io.IOBase):
            return value

        parsed_url = urllib.parse.urlparse(value)
        if parsed_url.scheme == "data":
            res = urllib.request.urlopen(value)  # noqa: S310
            return io.BytesIO(res.read())
        elif parsed_url.scheme == "http" or parsed_url.scheme == "https":
            return URLFile(value)
        else:
            raise ValueError(
                f"'{parsed_url.scheme}' is not a valid URL scheme. 'data', 'http', or 'https' is supported."
            )

    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        field_schema.update(type="string", format="uri")


class Path(pathlib.PosixPath):
    validate_always = True

    @classmethod
    def __get_validators__(cls) -> Iterator[Any]:
        yield cls.validate

    @classmethod
    def validate(cls, value: Any) -> pathlib.Path:
        if isinstance(value, pathlib.Path):
            return value
        if isinstance(value, io.IOBase):
            # this shouldn't happen in this path
            # Path is pretty much expected to be a string and not a file
            raise ValueError

        # get filename
        parsed_url = urllib.parse.urlparse(value)

        # this is kind of the the best place to convert, kinda
        # as long as you're converting to tempfile paths

        # this is also where you need to somehow note which tempfiles need to be filled
        if parsed_url.scheme == "data":
            return DataURLTempFilePath(value)
        if not (parsed_url.scheme == "http" or parsed_url.scheme == "https"):
            raise ValueError(
                f"'{parsed_url.scheme}' is not a valid URL scheme. 'data', 'http', or 'https' is supported."
            )
        return URLTempFile(value)

    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        field_schema.update(type="string", format="uri")


class URLTempFile(pathlib.PosixPath):
    """
    URLPath is a nasty hack to ensure that we can defer the downloading of a
    URL passed as a path until later in prediction dispatch.

    It subclasses pathlib.PosixPath only so that it can pass isinstance(_,
    pathlib.Path) checks.
    """

    _path: Optional[Path] = None

    def __init__(self, url: str) -> None:
        self.url = url
        self.filename = get_filename_from_url(url)

    async def convert(self, client: httpx.AsyncClient) -> Path:
        if self._path is None:
            dest = tempfile.NamedTemporaryFile(suffix=self.filename, delete=False)
            self._path = Path(dest.name)
            # I'd want to move the download elsewhere
            async with client.stream("GET", self.url) as resp:
                resp.raise_for_status()
                # resp.raw.decode_content = True
                async for chunk in resp.aiter_bytes():
                    dest.write(chunk)
        # this is our weird Path! that's weird!
        return self._path

    def __str__(self) -> str:
        # FastAPI's jsonable_encoder will encode subclasses of pathlib.Path by
        # calling str() on them
        return self.filename
        # honestly maybe returning self.url would be safer

    def unlink(self, missing_ok: bool = False) -> None:
        if self._path:
            # TODO: use unlink(missing_ok=...) when we drop Python 3.7 support.
            try:
                self._path.unlink()
            except FileNotFoundError:
                if not missing_ok:
                    raise


class DataURLTempFilePath(pathlib.PosixPath):
    def __init__(self, url: str) -> None:
        resp = urllib.request.urlopen(url)  # noqa: S310
        self.source = get_filename_from_urlopen(resp)
        dest = tempfile.NamedTemporaryFile(suffix=self.source, delete=False)
        shutil.copyfileobj(resp, dest)
        self._path = pathlib.Path(dest.name)

    def convert(self) -> pathlib.Path:
        return self._path

    def __str__(self) -> str:
        # FastAPI's jsonable_encoder will encode subclasses of pathlib.Path by
        # calling str() on them
        return self.source

    def unlink(self, missing_ok: bool = False) -> None:
        if self._path:
            # TODO: use unlink(missing_ok=...) when we drop Python 3.7 support.
            try:
                self._path.unlink()
            except FileNotFoundError:
                if not missing_ok:
                    raise


# we would prefer URLFile to stay lazy
# except... that doesn't really work with httpx?


class URLFile(io.IOBase):
    """
    URLFile is a proxy object for a :class:`urllib3.response.HTTPResponse`
    object that is created lazily. It's a file-like object constructed from a
    URL that can survive pickling/unpickling.

    This is the only place Cog uses requests
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


Item = TypeVar("Item")
_concatenate_iterator_schema = {
    "type": "array",
    "items": {"type": "string"},
    "x-cog-array-type": "iterator",
    "x-cog-array-display": "concatenate",
}


class ConcatenateIterator(Iterator[Item]):
    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        field_schema.pop("allOf", None)
        field_schema.update(_concatenate_iterator_schema)

    @classmethod
    def __get_validators__(cls) -> Iterator[Any]:
        yield cls.validate

    @classmethod
    def validate(cls, value: Iterator[Any]) -> Iterator[Any]:
        return value


class AsyncConcatenateIterator(AsyncIterator[Item]):
    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        field_schema.pop("allOf", None)
        field_schema.update(_concatenate_iterator_schema)

    @classmethod
    def __get_validators__(cls) -> Iterator[Any]:
        yield cls.validate

    @classmethod
    def validate(cls, value: AsyncIterator[Any]) -> AsyncIterator[Any]:
        return value


def _len_bytes(s: str, encoding: str = "utf-8") -> int:
    return len(s.encode(encoding))


def get_filename_from_urlopen(resp: urllib.response.addinfourl) -> str:
    mime_type = resp.headers.get_content_type()
    extension = mimetypes.guess_extension(mime_type)
    return ("file" + extension) if extension else "file"


def get_filename_from_url(url: str) -> str:
    parsed_url = urllib.parse.urlparse(url)

    filename = os.path.basename(parsed_url.path)
    filename = urllib.parse.unquote_plus(filename)

    # If the filename is too long, we truncate it (appending '~' to denote the
    # truncation) while preserving the file extension.
    # - truncate it
    # - append a tilde
    # - preserve the file extension
    if _len_bytes(filename) > FILENAME_MAX_LENGTH:
        filename = _truncate_filename_bytes(filename, length=FILENAME_MAX_LENGTH)

    for c in FILENAME_ILLEGAL_CHARS:
        filename = filename.replace(c, "_")
    return filename


def _truncate_filename_bytes(s: str, length: int, encoding: str = "utf-8") -> str:
    """
    Truncate a filename to at most `length` bytes, preserving file extension
    and avoiding text encoding corruption from truncation.
    """
    root, ext = os.path.splitext(s.encode(encoding))
    root = root[: length - len(ext) - 1]
    return root.decode(encoding, "ignore") + "~" + ext.decode(encoding)
