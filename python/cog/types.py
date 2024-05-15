import io
import mimetypes
import os
import pathlib
import shutil
import tempfile
import urllib.parse
import urllib.request
from abc import abstractmethod
from typing import TYPE_CHECKING, Any, Dict, Iterator, Optional, Type, TypeVar, Union

import pydantic
import requests
from pydantic import Field, SecretStr

if pydantic.__version__.startswith("1."):
    PYDANTIC_V2 = False
else:
    PYDANTIC_V2 = True
    if TYPE_CHECKING:
        from pydantic.json_schema import JsonSchemaValue  # type: ignore
        from pydantic_core.core_schema import CoreSchema  # type: ignore

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
    choices: "list[Union[str, int]]" = None,
) -> Any:
    """Input is similar to pydantic.Field, but doesn't require a default value to be the first argument."""
    kw = {}
    if regex:
        kw["pattern" if PYDANTIC_V2 else "regex"] = regex
    if choices:
        if PYDANTIC_V2:
            kw["json_schema_extra"] = {"choices": choices}
        else:
            kw["choices"] = choices

    return Field(
        default,
        description=description,
        ge=ge,
        le=le,
        min_length=min_length,
        max_length=max_length,
        **kw,
    )


class _SchemaMixin:
    if PYDANTIC_V2:

        @classmethod
        def __get_pydantic_json_schema__(
            cls, core_schema: "CoreSchema", handler: "pydantic.GetJsonSchemaHandler"
        ) -> "JsonSchemaValue":  # type: ignore
            json_schema = handler.resolve_ref_schema(handler(core_schema))
            cls._modify_schema(json_schema)
            return json_schema

    else:

        @classmethod
        def __modify_schema__(cls, field_schema: "dict[str, Any]") -> None:
            """Defines what this type should be in openapi.json"""
            cls._modify_schema(field_schema)

    @classmethod
    @abstractmethod
    def _modify_schema(cls, field_schema: "dict[str, Any]") -> None:
        raise NotImplementedError


if PYDANTIC_V2:
    from pydantic_core.core_schema import no_info_plain_validator_function

    class _ValidatorMixin:
        @classmethod
        def __get_pydantic_core_schema__(
            cls, source: Type[Any], handler: Any
        ) -> "CoreSchema":
            return no_info_plain_validator_function(cls.validate)

        @classmethod
        @abstractmethod
        def validate(cls, value: Any) -> Any:
            raise NotImplementedError

    # _ValidatorMixin = _ValidatorMixinV2

else:

    class _ValidatorMixin:
        validate_always = True

        @classmethod
        def __get_validators__(cls) -> Iterator[Any]:
            yield cls.validate

        @classmethod
        @abstractmethod
        def validate(cls, value: Any) -> Any:
            raise NotImplementedError

    # _ValidatorMixin = _ValidatorMixinV1


class Secret(SecretStr, _SchemaMixin):
    @classmethod
    def _modify_schema(cls, field_schema: "dict[str, Any]") -> None:
        """Defines what this type should be in openapi.json"""
        field_schema.update(
            {
                "type": "string",
                "format": "password",
                "x-cog-secret": True,
            }
        )


class File(io.IOBase, _SchemaMixin, _ValidatorMixin):
    """Deprecated: use Path instead."""

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
    def _modify_schema(cls, field_schema: "dict[str, Any]") -> None:
        """Defines what this type should be in openapi.json"""
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        field_schema.update(type="string", format="uri")


class Path(pathlib.PosixPath, _SchemaMixin, _ValidatorMixin):
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
    def _modify_schema(cls, field_schema: Dict[str, Any]) -> None:
        """Defines what this type should be in openapi.json"""
        # https://json-schema.org/understanding-json-schema/reference/string.html#uri-template
        field_schema.update(type="string", format="uri")


class URLPath(pathlib.PosixPath):
    """
    URLPath is a nasty hack to ensure that we can defer the downloading of a
    URL passed as a path until later in prediction dispatch.

    It subclasses pathlib.PosixPath only so that it can pass isinstance(_,
    pathlib.Path) checks.
    """

    _path: Optional[Path]

    def __init__(self, *, source: str, filename: str, fileobj: io.IOBase) -> None:
        self.source = source
        self.filename = filename
        self.fileobj = fileobj

        self._path = None

    def convert(self) -> Path:
        if self._path is None:
            dest = tempfile.NamedTemporaryFile(suffix=self.filename, delete=False)
            shutil.copyfileobj(self.fileobj, dest)
            self._path = Path(dest.name)
        return self._path

    def unlink(self, missing_ok: bool = False) -> None:
        if self._path:
            # TODO: use unlink(missing_ok=...) when we drop Python 3.7 support.
            try:
                self._path.unlink()
            except FileNotFoundError:
                if not missing_ok:
                    raise

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


Item = TypeVar("Item")


class ConcatenateIterator(Iterator[Item], _SchemaMixin, _ValidatorMixin):
    @classmethod
    def _modify_schema(cls, field_schema: Dict[str, Any]) -> None:
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
    def validate(cls, value: Iterator[Any]) -> Iterator[Any]:
        return value


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
