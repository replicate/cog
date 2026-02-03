"""
Internal ADT (Abstract Data Types) for predictor introspection.

This module defines the type system used internally for introspecting
predictor inputs and outputs, generating OpenAPI schemas, and validating
input values.
"""

import dataclasses
import os
import typing
from dataclasses import dataclass
from enum import Enum, auto
from typing import Any, Callable, Dict, List, Optional, Set, Union

from .coder import Coder
from .types import File, Path, Secret


def _type_name(tpe: Any) -> str:
    """Get a human-readable name for a type."""
    try:
        return tpe.__name__
    except AttributeError:
        return str(tpe)


def _is_union(tpe: type) -> bool:
    """Check if a type is a Union type."""
    if typing.get_origin(tpe) is Union:
        return True
    # Python 3.10+ has UnionType for X | Y syntax
    from types import UnionType

    if typing.get_origin(tpe) is UnionType:
        return True
    return False


class PrimitiveType(Enum):
    """Primitive types supported by Cog."""

    BOOL = auto()
    FLOAT = auto()
    INTEGER = auto()
    STRING = auto()
    PATH = auto()
    FILE = auto()  # Deprecated, use PATH
    SECRET = auto()
    ANY = auto()
    CUSTOM = auto()

    @staticmethod
    def _python_type() -> Dict["PrimitiveType", type]:
        return {
            PrimitiveType.BOOL: bool,
            PrimitiveType.FLOAT: float,
            PrimitiveType.INTEGER: int,
            PrimitiveType.STRING: str,
            PrimitiveType.PATH: Path,
            PrimitiveType.FILE: File,
            PrimitiveType.SECRET: Secret,
            PrimitiveType.ANY: Any,
            PrimitiveType.CUSTOM: Any,
        }

    @staticmethod
    def _json_type() -> Dict["PrimitiveType", str]:
        return {
            PrimitiveType.BOOL: "boolean",
            PrimitiveType.FLOAT: "number",
            PrimitiveType.INTEGER: "integer",
            PrimitiveType.STRING: "string",
            PrimitiveType.PATH: "string",
            PrimitiveType.FILE: "string",
            PrimitiveType.SECRET: "string",
            PrimitiveType.ANY: "object",
            PrimitiveType.CUSTOM: "object",
        }

    @staticmethod
    def _adt_type() -> Dict[type, "PrimitiveType"]:
        return {
            bool: PrimitiveType.BOOL,
            float: PrimitiveType.FLOAT,
            int: PrimitiveType.INTEGER,
            str: PrimitiveType.STRING,
            Path: PrimitiveType.PATH,
            File: PrimitiveType.FILE,
            Secret: PrimitiveType.SECRET,
            Any: PrimitiveType.ANY,
        }

    @staticmethod
    def from_type(tpe: type) -> "PrimitiveType":
        """Determine the PrimitiveType for a given Python type."""
        if match := PrimitiveType._adt_type().get(tpe):
            return match

        try:
            if tpe is os.PathLike or issubclass(tpe, os.PathLike):
                return PrimitiveType.PATH
        except TypeError:
            # issubclass raises TypeError for non-class types
            pass

        return PrimitiveType.CUSTOM

    def normalize(self, value: Any) -> Any:
        """Normalize a value to this primitive type."""
        pt = PrimitiveType._python_type()[self]
        tpe = type(value)

        if self is PrimitiveType.CUSTOM:
            return value
        elif self is PrimitiveType.ANY:
            return value
        elif self is PrimitiveType.FILE:
            # For File inputs, convert URL strings to file-like objects immediately
            # using File.validate() - the worker won't need to do any conversion
            import io

            if isinstance(value, io.IOBase):
                return value
            # URL string or data URI - validate to file-like object
            return File.validate(value)
        elif self is PrimitiveType.PATH:
            # Convert strings/URLs to Path or URLPath objects
            if isinstance(value, Path):
                return value
            return Path.validate(value)
        elif self is PrimitiveType.SECRET:
            # Convert strings to Secret objects
            if isinstance(value, Secret):
                return value
            return Secret(value)
        else:
            # Handle enums by extracting their value
            if issubclass(tpe, Enum):
                if not issubclass(tpe, pt):
                    raise ValueError(
                        f"enum {_type_name(tpe)} is used as {_type_name(pt)} "
                        "but does not extend it"
                    )
                value = value.value
            v = pt(value)
            # For numeric types, allow string coercion (e.g., "3" -> 3)
            # but verify the conversion is valid (not lossy for floats)
            if v != value:
                # Allow string to numeric conversion
                if isinstance(value, str) and pt in (int, float):
                    return v
                # Allow int to float conversion
                if isinstance(value, int) and pt is float:
                    return v
                raise ValueError(f"failed to normalize value as {_type_name(pt)}")
            return v

    def python_type_name(self) -> str:
        """Get the Python type name for this primitive."""
        return _type_name(PrimitiveType._python_type()[self])

    def json_type(self) -> Dict[str, Any]:
        """Get the JSON Schema type for this primitive."""
        jt: Dict[str, Any] = {"type": self._json_type()[self]}
        if self in {PrimitiveType.PATH, PrimitiveType.FILE}:
            jt["format"] = "uri"
        elif self is PrimitiveType.SECRET:
            jt["format"] = "password"
            jt["writeOnly"] = True
            jt["x-cog-secret"] = True
        return jt

    def json_encode(self, value: Any) -> Any:
        """Encode a value for JSON serialization."""
        if self is PrimitiveType.FLOAT:
            return float(value)
        elif self in {PrimitiveType.PATH, PrimitiveType.FILE}:
            return value
        elif self is PrimitiveType.SECRET:
            # Secret objects need to be unwrapped for JSON serialization
            if isinstance(value, Secret):
                return value.get_secret_value()
            return value
        elif self is PrimitiveType.ANY:
            return value
        return value


class Repetition(Enum):
    """Field repetition/optionality."""

    REQUIRED = 1
    OPTIONAL = 2
    REPEATED = 3


@dataclass(frozen=True)
class FieldType:
    """Type information for an input/output field."""

    primitive: PrimitiveType
    repetition: Repetition
    coder: Optional[Coder]

    @staticmethod
    def from_type(tpe: type) -> "FieldType":
        """Create a FieldType from a Python type annotation."""
        origin = typing.get_origin(tpe)

        # Handle bare collection types
        if tpe is list:
            tpe = List[Any]
            origin = typing.get_origin(tpe)
        elif tpe is dict:
            tpe = Dict[str, Any]
            origin = typing.get_origin(tpe)
        elif tpe is set:
            tpe = Set[Any]
            origin = typing.get_origin(tpe)

        if origin in (list, List):
            t_args = typing.get_args(tpe)
            if t_args:
                if len(t_args) != 1:
                    raise ValueError("List must have one type argument")
                elem_t = t_args[0]
                nested_t = typing.get_origin(elem_t)
                if nested_t is not None:
                    raise ValueError(
                        f"List cannot have nested type {_type_name(nested_t)}"
                    )
            else:
                elem_t = Any
            repetition = Repetition.REPEATED

        elif _is_union(tpe):
            t_args = typing.get_args(tpe)
            if not (len(t_args) == 2 and type(None) in t_args):
                raise ValueError(f"unsupported union type {tpe}")
            elem_t = t_args[0] if t_args[1] is type(None) else t_args[1]
            nested_t = typing.get_origin(elem_t)
            if nested_t is not None:
                raise ValueError(
                    f"Optional cannot have nested type {_type_name(nested_t)}"
                )
            repetition = Repetition.OPTIONAL

        else:
            elem_t = tpe
            repetition = Repetition.REQUIRED

        cog_t = PrimitiveType.from_type(elem_t)
        coder = None
        if cog_t is PrimitiveType.CUSTOM:
            coder = Coder.lookup(elem_t)
            if coder is None:
                raise ValueError(f"unsupported Cog type {_type_name(elem_t)}")

        return FieldType(primitive=cog_t, repetition=repetition, coder=coder)

    def normalize(self, value: Any) -> Any:
        """Normalize a value according to this field type."""
        if self.repetition is Repetition.REQUIRED:
            return self.primitive.normalize(value)
        elif self.repetition is Repetition.OPTIONAL:
            return None if value is None else self.primitive.normalize(value)
        elif self.repetition is Repetition.REPEATED:
            return [self.primitive.normalize(v) for v in value]
        return value

    def json_type(self) -> Dict[str, Any]:
        """Get the JSON Schema type for this field."""
        if self.repetition is Repetition.REPEATED:
            return {"type": "array", "items": self.primitive.json_type()}
        return self.primitive.json_type()

    def json_encode(self, value: Any) -> Any:
        """Encode a value for JSON serialization."""
        f: Callable[[Any], Any] = self.primitive.json_encode
        if self.primitive is PrimitiveType.CUSTOM:
            assert self.coder is not None
            f = self.coder.encode
        if self.repetition is Repetition.REPEATED:
            return [f(x) for x in value]
        return f(value)

    def json_decode(self, value: Any) -> Any:
        """Decode a value from JSON."""
        if self.primitive is not PrimitiveType.CUSTOM:
            return value
        assert self.coder is not None
        f = self.coder.decode
        if self.repetition is Repetition.REPEATED:
            return [f(x) for x in value]
        return f(value)

    def python_type_name(self) -> str:
        """Get the Python type name for this field."""
        if self.repetition is Repetition.REQUIRED:
            return self.primitive.python_type_name()
        elif self.repetition is Repetition.OPTIONAL:
            return f"Optional[{self.primitive.python_type_name()}]"
        elif self.repetition is Repetition.REPEATED:
            return f"List[{self.primitive.python_type_name()}]"
        return self.primitive.python_type_name()


@dataclass(frozen=True)
class InputField:
    """Metadata for a predictor input field."""

    name: str
    order: int
    type: FieldType
    default: Any = None
    description: Optional[str] = None
    ge: Optional[Union[int, float]] = None
    le: Optional[Union[int, float]] = None
    min_length: Optional[int] = None
    max_length: Optional[int] = None
    regex: Optional[str] = None
    choices: Optional[List[Union[str, int]]] = None
    deprecated: Optional[bool] = None


class OutputKind(Enum):
    """Kind of output a predictor produces."""

    SINGLE = 1
    LIST = 2
    ITERATOR = 3
    CONCAT_ITERATOR = 4
    OBJECT = 5


@dataclass(frozen=True)
class OutputType:
    """Type information for predictor output."""

    kind: OutputKind
    type: Optional[PrimitiveType] = None
    fields: Optional[Dict[str, FieldType]] = None
    coder: Optional[Coder] = None

    def json_type(self) -> Dict[str, Any]:
        """Get the JSON Schema type for this output."""
        jt: Dict[str, Any] = {"title": "Output"}

        if self.kind is OutputKind.SINGLE:
            assert self.type is not None
            jt.update(self.type.json_type())

        elif self.kind is OutputKind.LIST:
            assert self.type is not None
            jt.update({"type": "array", "items": self.type.json_type()})

        elif self.kind is OutputKind.ITERATOR:
            assert self.type is not None
            jt.update(
                {
                    "type": "array",
                    "items": self.type.json_type(),
                    "x-cog-array-type": "iterator",
                }
            )

        elif self.kind is OutputKind.CONCAT_ITERATOR:
            assert self.type is not None
            jt.update(
                {
                    "type": "array",
                    "items": self.type.json_type(),
                    "x-cog-array-type": "iterator",
                    "x-cog-array-display": "concatenate",
                }
            )

        elif self.kind is OutputKind.OBJECT:
            assert self.fields is not None
            props = {}
            for name, field_type in self.fields.items():
                props[name] = field_type.primitive.json_type()
                props[name]["title"] = name.replace("_", " ").title()
            jt.update(
                {
                    "type": "object",
                    "properties": props,
                    "required": list(self.fields.keys()),
                }
            )

        return jt

    def normalize(self, value: Any) -> Any:
        """Normalize an output value."""
        return self._transform(value, json=False)

    def json_encode(self, value: Any) -> Any:
        """Encode an output value for JSON serialization."""
        if self.coder is not None:
            if self.kind is OutputKind.LIST:
                return [self.coder.encode(x) for x in value]
            return self.coder.encode(value)

        o = self._transform(value, json=True)
        if self.kind is OutputKind.OBJECT:
            # Expand dataclass to dict
            tpe = type(o)
            if not dataclasses.is_dataclass(tpe):
                raise ValueError(f"{tpe} is not a dataclass")
            return {f.name: getattr(o, f.name) for f in dataclasses.fields(o)}
        return o

    def _transform(self, value: Any, json: bool) -> Any:
        """Transform an output value (normalize or encode)."""
        if self.kind in {
            OutputKind.SINGLE,
            OutputKind.ITERATOR,
            OutputKind.CONCAT_ITERATOR,
        }:
            assert self.type is not None
            f: Callable[[Any], Any] = (
                self.type.json_encode if json else self.type.normalize
            )
            return f(value)

        elif self.kind is OutputKind.LIST:
            assert self.type is not None
            f = self.type.json_encode if json else self.type.normalize
            return [f(x) for x in value]

        elif self.kind is OutputKind.OBJECT:
            assert self.fields is not None
            for name, ft in self.fields.items():
                f = ft.json_encode if json else ft.normalize
                if not hasattr(value, name):
                    raise ValueError(f"missing output field: {name}")
                v = getattr(value, name)
                if v is None:
                    if ft.repetition is not Repetition.OPTIONAL:
                        raise ValueError(f"missing value for output field: {name}")
                setattr(value, name, f(v))
            return value

        raise RuntimeError(f"unsupported output kind {self.kind}")


@dataclass(frozen=True)
class PredictorInfo:
    """Complete type information for a predictor."""

    module_name: str
    predictor_name: str
    inputs: Dict[str, InputField]
    output: OutputType
