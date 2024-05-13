import io
import mimetypes
import os
import pathlib
import shutil
import tempfile
import urllib.parse
import urllib.request
from typing import Any, Dict, Iterator, List, Optional, TypeVar, Union

import requests
from pydantic import Field, SecretStr
from .types_shared import URLFile, URLPath, get_filename

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

        return URLPath(
            source=value,
            filename=get_filename(value),
            fileobj=File.validate(value),
        )

    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        field_schema.update(type="string", format="uri")


Item = TypeVar("Item")


class ConcatenateIterator(Iterator[Item]):
    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        field_schema.pop("allOf", None)
        field_schema.update(
            {
                "type": "array",
                "items": {"type": "string"},
                "x-cog-array-type": "iterator",
                "x-cog-array-display": "concatenate",
            }
        )

    @classmethod
    def __get_validators__(cls) -> Iterator[Any]:
        yield cls.validate

    @classmethod
    def validate(cls, value: Iterator[Any]) -> Iterator[Any]:
        return value
