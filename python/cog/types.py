import io
import mimetypes
import os
import base64
import pathlib
import requests
import shutil
import tempfile
from typing import Any, Callable, Dict, Iterator, List, Union
from urllib.parse import urlparse

from pydantic import Field
from pydantic.typing import NoArgAnyCallable


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


class File(io.IOBase):
    validate_always = True

    @classmethod
    def __get_validators__(cls) -> Iterator[Any]:
        yield cls.validate

    @classmethod
    def validate(cls, value: Any) -> io.IOBase:
        if isinstance(value, io.IOBase):
            return value

        parsed_url = urlparse(value)
        if parsed_url.scheme == "data":
            # https://developer.mozilla.org/en-US/docs/Web/HTTP/Basics_of_HTTP/Data_URIs
            # TODO: decode properly. this maybe? https://github.com/fcurella/python-datauri/
            header, encoded = parsed_url.path.split(",", 1)
            return io.BytesIO(base64.b64decode(encoded))
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

        src = File.validate(value)
        dest = tempfile.NamedTemporaryFile(suffix=get_filename(value), delete=False)
        shutil.copyfileobj(src, dest)
        return cls(dest.name)

    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        field_schema.update(type="string", format="uri")


class URLFile(io.IOBase):
    """
    URLFile is a proxy object for a :class:`urllib3.response.HTTPResponse`
    object that is created lazily. It's a file-like object constructed from a
    URL that can survive pickling/unpickling.
    """

    __slots__ = ("__target__", "__url__")

    def __init__(self, url):
        object.__setattr__(self, "__url__", url)

    # We provide __getstate__ and __setstate__ explicitly to ensure that the
    # object is always picklable.
    def __getstate__(self):
        return {"url": object.__getattribute__(self, "__url__")}

    def __setstate__(self, state):
        object.__setattr__(self, "__url__", state["url"])

    # Proxy getattr/setattr/delattr through to the response object.
    def __setattr__(self, name, value):
        if hasattr(type(self), name):
            object.__setattr__(self, name, value)
        else:
            setattr(self.__wrapped__, name, value)

    def __getattr__(self, name):
        if name in ("__target__", "__wrapped__", "__url__"):
            raise AttributeError(name)
        else:
            return getattr(self.__wrapped__, name)

    def __delattr__(self, name):
        if hasattr(type(self), name):
            object.__delattr__(self, name)
        else:
            delattr(self.__wrapped__, name)

    # Luckily the only dunder method on HTTPResponse is __iter__
    def __iter__(self):
        return iter(self.__wrapped__)

    @property
    def __wrapped__(self):
        try:
            return object.__getattribute__(self, "__target__")
        except AttributeError:
            url = object.__getattribute__(self, "__url__")
            resp = requests.get(url, stream=True)
            resp.raise_for_status()
            resp.raw.decode_content = True
            object.__setattr__(self, "__target__", resp.raw)
            return resp.raw

    def __repr__(self):
        try:
            target = object.__getattribute__(self, "__target__")
        except AttributeError:
            return "<{} at 0x{:x} for {!r}>".format(
                type(self).__name__, id(self), object.__getattribute__(self, "__url__")
            )
        else:
            return "<{} at 0x{:x} wrapping {!r}>".format(
                type(self).__name__, id(self), target, id(target)
            )


def get_filename(url: str) -> str:
    parsed_url = urlparse(url)
    if parsed_url.scheme == "data":
        header, _ = parsed_url.path.split(",", 1)
        mime_type, _ = header.split(";", 1)
        extension = mimetypes.guess_extension(mime_type)
        if extension is None:
            return "file"
        return "file" + extension
    return os.path.basename(parsed_url.path)
