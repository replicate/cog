import io
import mimetypes
import os
import base64
import pathlib
import requests
import shutil
import tempfile
from typing import Any, List, Optional
from urllib.parse import urlparse

from pydantic import Field
from pydantic.typing import NoArgAnyCallable


def Input(
    default=...,
    description: str = None,
    ge: float = None,
    le: float = None,
    min_length: int = None,
    max_length: int = None,
    regex: str = None,
    choices: List[str | int] = None,
):
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


def get_filename(url):
    parsed_url = urlparse(url)
    if parsed_url.scheme == "data":
        header, _ = parsed_url.path.split(",", 1)
        mime_type, _ = header.split(";", 1)
        return "file" + mimetypes.guess_extension(mime_type)
    return os.path.basename(parsed_url.path)


class File(io.IOBase):
    validate_always = True

    @classmethod
    def __get_validators__(cls):
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
            resp = requests.get(value, stream=True)
            resp.raise_for_status()
            resp.raw.decode_content = True
            return resp.raw
        else:
            raise ValueError(
                f"'{parsed_url.scheme}' is not a valid URL scheme. 'data', 'http', or 'https' is supported."
            )

    @classmethod
    def __modify_schema__(cls, field_schema):
        """Defines what this type should be in openapi.json"""
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        field_schema.update(type="string", format="uri")


class Path(pathlib.PosixPath):
    validate_always = True

    @classmethod
    def __get_validators__(cls):
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
    def __modify_schema__(cls, field_schema):
        """Defines what this type should be in openapi.json"""
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        field_schema.update(type="string", format="uri")
