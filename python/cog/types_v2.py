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
from pydantic import Field, GetJsonSchemaHandler, SecretStr
from pydantic.json_schema import JsonSchemaValue
from pydantic_core import core_schema as cs
from pydantic_core.core_schema import no_info_plain_validator_function

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
    def __get_pydantic_json_schema__(
        cls, core_schema: cs.CoreSchema, handler: GetJsonSchemaHandler
    ) -> JsonSchemaValue:
        json_schema = handler.resolve_ref_schema(handler(core_schema))
        json_schema["x-cog-secret"] = True
        return json_schema


class File(io.IOBase):
    """Deprecated: use Path instead."""

    # removed in pydantic 2 with no replacement?
    # validate_always = True

    @classmethod
    def __get_pydantic_core_schema__(
        cls, source: Type[Any], handler: Callable[[Any], CoreSchema]
    ) -> CoreSchema:
        return no_info_plain_validator_function(cls.validate)

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
    def __get_pydantic_json_schema__(
        cls, core_schema: cs.CoreSchema, handler: GetJsonSchemaHandler
    ) -> JsonSchemaValue:
        """Defines what this type should be in openapi.json"""
        json_schema = handler.resolve_ref_schema(handler(core_schema))
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        json_schema.update(type="string", format="uri")
        return json_schema


class Path(pathlib.PosixPath):
    # validate_always = True

    @classmethod
    def __get_pydantic_core_schema__(
        cls, source: Type[Any], handler: Callable[[Any], CoreSchema]
    ) -> CoreSchema:
        return no_info_plain_validator_function(cls.validate)

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
    def __get_pydantic_json_schema__(
        cls, core_schema: cs.CoreSchema, handler: GetJsonSchemaHandler
    ) -> JsonSchemaValue:
        """Defines what this type should be in openapi.json"""
        json_schema = handler.resolve_ref_schema(handler(core_schema))
        json_schema.update(type="string", format="uri")
        return json_schema


Item = TypeVar("Item")


class ConcatenateIterator(Iterator[Item]):
    @classmethod
    def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        json_schema.pop("allOf", None)
        json_schema.update(
            {
                "type": "array",
                "items": {"type": "string"},
                "x-cog-array-type": "iterator",
                "x-cog-array-display": "concatenate",
            }
        )
        return json_schema

    # this seems to be a no-op
    # @classmethod
    # def __get_validators__(cls) -> Iterator[Any]:
    #     yield cls.validate
    # @classmethod
    # def validate(cls, value: Iterator[Any]) -> Iterator[Any]:
    #     return value
