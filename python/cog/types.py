import io
import mimetypes
import os
import pathlib
import shutil
import tempfile
import urllib.parse
import urllib.request
import urllib.response
from typing import (
    Any,
    AsyncIterator,
    Dict,
    Iterator,
    List,
    Optional,
    Type,
    TypedDict,
    TypeVar,
    Union,
)

import pydantic
import requests
from typing_extensions import NotRequired  # added to typing in python 3.11

if pydantic.__version__.startswith("1."):
    PYDANTIC_V2 = False
else:
    PYDANTIC_V2 = True


FILENAME_ILLEGAL_CHARS = set("\u0000/")

# Linux allows files up to 255 bytes long. We enforce a slightly shorter
# filename so that there's room for prefixes added by
# tempfile.NamedTemporaryFile, etc.
FILENAME_MAX_LENGTH = 200


class ExperimentalFeatureWarning(Warning):
    pass


class CogConfig(TypedDict):  # pylint: disable=too-many-ancestors
    build: "CogBuildConfig"
    concurrency: "CogConcurrencyConfig"
    image: NotRequired[str]
    predict: NotRequired[str]
    train: NotRequired[str]


class CogBuildConfig(TypedDict, total=False):  # pylint: disable=too-many-ancestors
    cuda: Optional[str]
    gpu: Optional[bool]
    python_packages: Optional[List[str]]
    system_packages: Optional[List[str]]
    python_requirements: Optional[str]
    python_version: Optional[str]
    run: Optional[Union[List[str], List[Dict[str, Any]]]]


class CogConcurrencyConfig(TypedDict, total=False):  # pylint: disable=too-many-ancestors
    max: NotRequired[int]


def Input(  # pylint: disable=invalid-name, too-many-arguments
    default: Any = ...,
    description: Optional[str] = None,
    ge: Optional[float] = None,
    le: Optional[float] = None,
    min_length: Optional[int] = None,
    max_length: Optional[int] = None,
    regex: Optional[str] = None,
    choices: Optional[List[Union[str, int]]] = None,
    deprecated: Optional[bool] = None,
) -> Any:
    """Input is similar to pydantic.Field, but doesn't require a default value to be the first argument."""
    field_kwargs = {
        "default": default,
        "description": description,
        "ge": ge,
        "le": le,
        "min_length": min_length,
        "max_length": max_length,
    }

    if PYDANTIC_V2:
        field_kwargs["pattern"] = regex
        if choices:
            # The `choices` parameter is deprecated in Pydantic v2.
            # Instead, the user should use `Literal[...]`
            # to specify the allowed values.
            field_kwargs["json_schema_extra"] = {"enum": choices}
    else:
        field_kwargs["regex"] = regex
        field_kwargs["enum"] = choices

    if deprecated is not None:
        field_kwargs["deprecated"] = deprecated

    return pydantic.Field(**field_kwargs)


class Secret(pydantic.SecretStr):
    if PYDANTIC_V2:
        from pydantic.json_schema import JsonSchemaValue
        from pydantic_core import CoreSchema

        @classmethod
        def __get_pydantic_json_schema__(
            cls, core_schema: CoreSchema, handler: Any
        ) -> JsonSchemaValue:
            json_schema = handler(core_schema)
            json_schema.update(
                {
                    "type": "string",
                    "format": "password",
                    "x-cog-secret": True,
                }
            )
            return json_schema

    else:

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
    def validate(cls, value: Any) -> io.IOBase:
        if isinstance(value, io.IOBase):
            return value

        parsed_url = urllib.parse.urlparse(value)
        if parsed_url.scheme == "data":
            with urllib.request.urlopen(value) as res:  # noqa: S310
                return io.BytesIO(res.read())
        if parsed_url.scheme in ("http", "https"):
            return URLFile(value)
        raise ValueError(
            f"'{parsed_url.scheme}' is not a valid URL scheme. 'data', 'http', or 'https' is supported."
        )

    if PYDANTIC_V2:
        from pydantic import GetCoreSchemaHandler, TypeAdapter
        from pydantic_core.core_schema import CoreSchema

        @classmethod
        def __get_pydantic_core_schema__(
            cls,
            source: Type[Any],  # pylint: disable=unused-argument
            handler: "pydantic.GetCoreSchemaHandler",  # pylint: disable=unused-argument
        ) -> "CoreSchema":
            from pydantic_core import (  # pylint: disable=import-outside-toplevel
                core_schema,
            )

            return core_schema.union_schema(
                [
                    core_schema.is_instance_schema(io.IOBase),
                    core_schema.no_info_plain_validator_function(cls.validate),
                ]
            )

        @classmethod
        def __get_pydantic_json_schema__(
            cls, core_schema: "CoreSchema", handler: "pydantic.GetJsonSchemaHandler"
        ) -> "JsonSchemaValue":  # type: ignore # noqa: F821
            json_schema = handler(core_schema)
            json_schema.update(type="string", format="uri")
            return json_schema

    else:

        @classmethod
        def __get_validators__(cls) -> Iterator[Any]:
            yield cls.validate

        @classmethod
        def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
            """Defines what this type should be in openapi.json"""
            # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
            field_schema.update(type="string", format="uri")


class Path(pathlib.PosixPath):  # pylint: disable=abstract-method
    validate_always = True

    @classmethod
    def validate(cls, value: Any) -> pathlib.Path:
        if isinstance(value, pathlib.Path):
            return value

        return URLPath(
            source=value,
            filename=get_filename(value),
            fileobj=File.validate(value),
        )

    if PYDANTIC_V2:
        from pydantic import GetCoreSchemaHandler
        from pydantic.json_schema import JsonSchemaValue
        from pydantic_core import CoreSchema

        @classmethod
        def __get_pydantic_core_schema__(
            cls,
            source: Type[Any],  # pylint: disable=unused-argument
            handler: "pydantic.GetCoreSchemaHandler",  # pylint: disable=unused-argument
        ) -> "CoreSchema":
            from pydantic_core import (  # pylint: disable=import-outside-toplevel
                core_schema,
            )

            return core_schema.union_schema(
                [
                    core_schema.is_instance_schema(pathlib.Path),
                    core_schema.no_info_plain_validator_function(cls.validate),
                ]
            )

        @classmethod
        def __get_pydantic_json_schema__(
            cls, core_schema: "CoreSchema", handler: "pydantic.GetJsonSchemaHandler"
        ) -> "JsonSchemaValue":  # type: ignore # noqa: F821
            json_schema = handler(core_schema)
            json_schema.update(type="string", format="uri")
            return json_schema

    else:

        @classmethod
        def __get_validators__(cls) -> Iterator[Any]:
            yield cls.validate

        @classmethod
        def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
            """Defines what this type should be in openapi.json"""
            # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
            field_schema.update(type="string", format="uri")


class URLPath(pathlib.PosixPath):  # pylint: disable=abstract-method
    """
    URLPath is a nasty hack to ensure that we can defer the downloading of a
    URL passed as a path until later in prediction dispatch.

    It subclasses pathlib.PosixPath only so that it can pass isinstance(_,
    pathlib.Path) checks.
    """

    _path: Optional[Path]

    def __init__(self, *, source: str, filename: str, fileobj: io.IOBase) -> None:  # pylint: disable=super-init-not-called
        if len(filename) > FILENAME_MAX_LENGTH:
            filename = _truncate_filename_bytes(filename, FILENAME_MAX_LENGTH)

        self.source = source
        self.filename = filename
        self.fileobj = fileobj

        self._path = None

    def convert(self) -> Path:
        if self._path is None:
            dest = tempfile.NamedTemporaryFile(suffix=self.filename, delete=False)  # pylint: disable=consider-using-with
            shutil.copyfileobj(self.fileobj, dest)
            self._path = Path(dest.name)
        return self._path

    def unlink(self, missing_ok: bool = False) -> None:
        if self._path:
            self._path.unlink(missing_ok=missing_ok)

    def __str__(self) -> str:
        # FastAPI's jsonable_encoder will encode subclasses of pathlib.Path by
        # calling str() on them
        return self.source


class URLFile(io.IOBase):
    """
    URLFile is a proxy object for a :class:`urllib3.response.HTTPResponse`
    object that is created lazily. It's a file-like object constructed from a
    URL that can survive pickling/unpickling.
    """

    __slots__ = ("__target__", "__url__", "name")

    def __init__(self, url: str, filename: Optional[str] = None) -> None:
        parsed = urllib.parse.urlparse(url)
        if parsed.scheme not in {
            "http",
            "https",
        }:
            raise ValueError(
                "URLFile requires URL to conform to HTTP or HTTPS protocol"
            )
        object.__setattr__(self, "name", os.path.basename(parsed.path))
        object.__setattr__(self, "__url__", url)

        if parsed.scheme not in {
            "http",
            "https",
        }:
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


def get_filename(url: str) -> str:
    parsed_url = urllib.parse.urlparse(url)

    if parsed_url.scheme == "data":
        with urllib.request.urlopen(url) as resp:  # noqa: S310
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


Item = TypeVar("Item")
_concatenate_iterator_schema = {
    "type": "array",
    "items": {"type": "string"},
    "x-cog-array-type": "iterator",
    "x-cog-array-display": "concatenate",
}


class ConcatenateIterator(Iterator[Item]):  # pylint: disable=abstract-method
    @classmethod
    def validate(cls, value: Iterator[Any]) -> Iterator[Any]:
        return value

    if PYDANTIC_V2:
        from pydantic import GetCoreSchemaHandler
        from pydantic.json_schema import JsonSchemaValue
        from pydantic_core import CoreSchema

        @classmethod
        def __get_pydantic_core_schema__(
            cls,
            source: Type[Any],  # pylint: disable=unused-argument
            handler: "pydantic.GetCoreSchemaHandler",  # pylint: disable=unused-argument
        ) -> "CoreSchema":
            from pydantic_core import (  # pylint: disable=import-outside-toplevel
                core_schema,
            )

            return core_schema.union_schema(
                [
                    core_schema.is_instance_schema(Iterator),
                    core_schema.no_info_plain_validator_function(cls.validate),
                ]
            )

        @classmethod
        def __get_pydantic_json_schema__(
            cls, core_schema: "CoreSchema", handler: "pydantic.GetJsonSchemaHandler"
        ) -> "JsonSchemaValue":  # type: ignore # noqa: F821
            json_schema = handler(core_schema)
            json_schema.pop("allOf", None)
            json_schema.update(_concatenate_iterator_schema)
            return json_schema

    else:

        @classmethod
        def __get_validators__(cls) -> Iterator[Any]:
            yield cls.validate

        @classmethod
        def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
            """Defines what this type should be in openapi.json"""
            field_schema.pop("allOf", None)
            field_schema.update(_concatenate_iterator_schema)


class AsyncConcatenateIterator(AsyncIterator[Item]):
    @classmethod
    def validate(cls, value: AsyncIterator[Any]) -> AsyncIterator[Any]:
        return value

    if PYDANTIC_V2:
        from pydantic import GetCoreSchemaHandler
        from pydantic.json_schema import JsonSchemaValue
        from pydantic_core import CoreSchema

        @classmethod
        def __get_pydantic_core_schema__(
            cls,
            source: Type[Any],  # pylint: disable=unused-argument
            handler: "pydantic.GetCoreSchemaHandler",  # pylint: disable=unused-argument
        ) -> "CoreSchema":
            from pydantic_core import (  # pylint: disable=import-outside-toplevel
                core_schema,
            )

            return core_schema.union_schema(
                [
                    core_schema.is_instance_schema(AsyncIterator),
                    core_schema.no_info_plain_validator_function(cls.validate),
                ]
            )

        @classmethod
        def __get_pydantic_json_schema__(
            cls, core_schema: "CoreSchema", handler: "pydantic.GetJsonSchemaHandler"
        ) -> "JsonSchemaValue":  # type: ignore # noqa: F821
            json_schema = handler(core_schema)
            json_schema.pop("allOf", None)
            json_schema.update(_concatenate_iterator_schema)
            return json_schema
    else:

        @classmethod
        def __modify_schema__(cls, field_schema: Dict[str, Any]) -> None:
            """Defines what this type should be in openapi.json"""
            field_schema.pop("allOf", None)
            field_schema.update(_concatenate_iterator_schema)

        @classmethod
        def __get_validators__(cls) -> Iterator[Any]:
            yield cls.validate


Weights = Union[File, Path, str]


def get_filename_from_urlopen(resp: urllib.response.addinfourl) -> str:
    mime_type = resp.headers.get_content_type()
    extension = mimetypes.guess_extension(mime_type)
    return ("file" + extension) if extension else "file"


def _len_bytes(s: str, encoding: str = "utf-8") -> int:
    return len(s.encode(encoding))


def _truncate_filename_bytes(s: str, length: int, encoding: str = "utf-8") -> str:
    """
    Truncate a filename to at most `length` bytes, preserving file extension
    and avoiding text encoding corruption from truncation.
    """
    root, ext = os.path.splitext(s.encode(encoding))
    ext = ext.decode(encoding).split("?")[0].encode(encoding)
    root = root[: length - len(ext) - 1]
    return root.decode(encoding, "ignore") + "~" + ext.decode(encoding)
