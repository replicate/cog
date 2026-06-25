"""
Internal ADT (Abstract Data Types) for predictor introspection.

This module defines the type system used internally for introspecting
predictor inputs and outputs, and validating input values.
"""

import dataclasses
import os
import typing
from dataclasses import dataclass
from enum import Enum, auto
from typing import Any, Callable, Dict, List, Optional, Set, Union

from ._opaque import Opaque, _OpaqueMarker
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


def _is_none_type(tpe: Any) -> bool:
    return tpe is None or tpe is type(None)


def _is_dict_like(tpe: Any) -> bool:
    """Check if a type should be treated like a dict, including TypedDict."""
    if tpe is dict:
        return True

    try:
        from typing_extensions import is_typeddict as is_typeddict_ext

        if is_typeddict_ext(tpe):
            return True
    except ImportError:
        # `typing_extensions` is optional; fall back to `typing.is_typeddict` below.
        pass

    is_typeddict = getattr(typing, "is_typeddict", None)
    return bool(callable(is_typeddict) and is_typeddict(tpe))


def _unwrap_opaque(tpe: Any) -> tuple[Any, bool]:
    """Return (inner, True) for Annotated[inner, Opaque, ...]."""
    if typing.get_origin(tpe) is not typing.Annotated:
        return tpe, False

    args = typing.get_args(tpe)
    if len(args) < 2:
        return tpe, False

    inner = args[0]
    for meta in args[1:]:
        if meta is Opaque or isinstance(meta, _OpaqueMarker):
            return inner, True
    return tpe, False


def _opaque_field_type(inner: Any) -> "FieldType":
    """Build a FieldType for an opaque JSON object, preserving list/optional shape."""
    origin = typing.get_origin(inner)
    if inner is list or origin in (list, List):
        return FieldType(PrimitiveType.ANY, Repetition.REPEATED, None)

    if _is_union(inner):
        args = typing.get_args(inner)
        if len(args) == 2 and type(None) in args:
            non_none = args[0] if args[1] is type(None) else args[1]
            if non_none is list or typing.get_origin(non_none) in (list, List):
                return FieldType(PrimitiveType.ANY, Repetition.OPTIONAL_REPEATED, None)
            return FieldType(PrimitiveType.ANY, Repetition.OPTIONAL, None)

    return FieldType(PrimitiveType.ANY, Repetition.REQUIRED, None)


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
    def _python_type() -> Dict["PrimitiveType", type | Any]:
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
    def _adt_type() -> Dict[type | Any, "PrimitiveType"]:
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
    def from_type(tpe: type | Any) -> "PrimitiveType":
        """Determine the PrimitiveType for a given Python type."""
        if match := PrimitiveType._adt_type().get(tpe):
            return match

        try:
            if tpe is os.PathLike or (
                isinstance(tpe, type) and issubclass(tpe, os.PathLike)  # type: ignore[arg-type]
            ):
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
    OPTIONAL_REPEATED = 4  # list[X] | None


def _is_supported_union_variant(ft: "FieldType") -> bool:
    if ft.union_variants is not None:
        return False
    if ft.primitive in {
        PrimitiveType.PATH,
        PrimitiveType.FILE,
        PrimitiveType.SECRET,
        PrimitiveType.CUSTOM,
    }:
        return False
    return ft.repetition in {Repetition.REQUIRED, Repetition.REPEATED}


def _is_exact_union_match(value: Any, ft: "FieldType") -> bool:
    if ft.repetition is Repetition.REPEATED:
        return isinstance(value, list)
    if ft.repetition is not Repetition.REQUIRED:
        return False
    if ft.primitive is PrimitiveType.BOOL:
        return isinstance(value, bool)
    if ft.primitive is PrimitiveType.INTEGER:
        return isinstance(value, int) and not isinstance(value, bool)
    if ft.primitive is PrimitiveType.FLOAT:
        return isinstance(value, float)
    if ft.primitive is PrimitiveType.STRING:
        return isinstance(value, str)
    if ft.primitive is PrimitiveType.ANY:
        return isinstance(value, dict)
    return False


def _union_primitive_accepts_value(value: Any, primitive: PrimitiveType) -> bool:
    if primitive is PrimitiveType.BOOL:
        return type(value) is bool
    if primitive is PrimitiveType.INTEGER:
        return type(value) is int
    if primitive is PrimitiveType.FLOAT:
        return type(value) is float or type(value) is int
    if primitive is PrimitiveType.STRING:
        return type(value) is str
    if primitive is PrimitiveType.ANY:
        return isinstance(value, dict)
    return False


def _union_variant_accepts_value(value: Any, variant: "FieldType") -> bool:
    if variant.repetition is Repetition.REPEATED:
        return isinstance(value, list) and all(
            _union_primitive_accepts_value(element, variant.primitive)
            for element in value
        )
    if variant.repetition is not Repetition.REQUIRED:
        return False
    return _union_primitive_accepts_value(value, variant.primitive)


def _union_variant_priority(ft: "FieldType") -> int:
    primitive_priority = {
        PrimitiveType.BOOL: 0,
        PrimitiveType.INTEGER: 1,
        PrimitiveType.FLOAT: 2,
        PrimitiveType.STRING: 3,
        PrimitiveType.ANY: 4,
    }
    repetition_offset = 10 if ft.repetition is Repetition.REPEATED else 0
    return repetition_offset + primitive_priority.get(ft.primitive, 100)


def _ordered_union_variants(
    value: Any, variants: List["FieldType"]
) -> List["FieldType"]:
    return [
        variant
        for _, variant in sorted(
            enumerate(variants),
            key=lambda item: (
                not _is_exact_union_match(value, item[1]),
                _union_variant_priority(item[1]),
                item[0],
            ),
        )
    ]


@dataclass(frozen=True)
class FieldType:
    """Type information for an input/output field."""

    primitive: PrimitiveType
    repetition: Repetition
    coder: Optional[Coder]
    union_variants: Optional[List["FieldType"]] = None

    @staticmethod
    def from_type(tpe: type) -> "FieldType":
        """Create a FieldType from a Python type annotation."""
        inner, is_opaque = _unwrap_opaque(tpe)
        if is_opaque:
            return _opaque_field_type(inner)

        origin = typing.get_origin(tpe)

        # Handle bare collection types
        if tpe is list:
            tpe = List[Any]
            origin = typing.get_origin(tpe)
        elif _is_dict_like(tpe):
            tpe = Dict[str, Any]
            origin = typing.get_origin(tpe)
        elif tpe is set:
            tpe = Set[Any]
            origin = typing.get_origin(tpe)

        if origin is dict or _is_dict_like(tpe):
            # dict / Dict[K, V] → opaque JSON object, consistent with the
            # static Go schema generator's SchemaAnyType().
            return FieldType(
                primitive=PrimitiveType.ANY,
                repetition=Repetition.REQUIRED,
                coder=None,
            )

        if origin in (list, List):
            t_args = typing.get_args(tpe)
            if t_args:
                if len(t_args) != 1:
                    raise ValueError("List must have one type argument")
                elem_t = t_args[0]
                _, elem_is_opaque = _unwrap_opaque(elem_t)
                if elem_is_opaque:
                    return FieldType(
                        primitive=PrimitiveType.ANY,
                        repetition=Repetition.REPEATED,
                        coder=None,
                    )
                # dict elements in lists → treat as ANY (opaque JSON objects)
                if _is_dict_like(elem_t) or typing.get_origin(elem_t) is dict:
                    return FieldType(
                        primitive=PrimitiveType.ANY,
                        repetition=Repetition.REPEATED,
                        coder=None,
                    )
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
            has_none = any(_is_none_type(arg) for arg in t_args)
            non_none_args = [arg for arg in t_args if not _is_none_type(arg)]

            if len(non_none_args) != 1:
                repetition = Repetition.OPTIONAL if has_none else Repetition.REQUIRED
                variants = []
                for arg in non_none_args:
                    try:
                        variant = FieldType.from_type(arg)
                    except ValueError as exc:
                        raise ValueError(
                            f"unsupported union member {_type_name(arg)} in union {tpe}"
                        ) from exc
                    if not _is_supported_union_variant(variant):
                        raise ValueError(
                            f"unsupported union member {_type_name(arg)} in union {tpe}"
                        )
                    variants.append(variant)
                return FieldType(
                    primitive=PrimitiveType.ANY,
                    repetition=repetition,
                    coder=None,
                    union_variants=variants,
                )

            if not has_none:
                elem_t = non_none_args[0]
                repetition = Repetition.REQUIRED
                cog_t = PrimitiveType.from_type(elem_t)
                coder = None
                if cog_t is PrimitiveType.CUSTOM:
                    coder = Coder.lookup(elem_t)
                    if coder is None:
                        raise ValueError(f"unsupported Cog type {_type_name(elem_t)}")
                return FieldType(primitive=cog_t, repetition=repetition, coder=coder)

            elem_t = non_none_args[0]
            inner, elem_is_opaque = _unwrap_opaque(elem_t)
            if elem_is_opaque:
                return _opaque_field_type(inner | None)
            nested_t = typing.get_origin(elem_t)
            if nested_t in (list, List):
                # list[X] | None  →  optional repeated
                list_args = typing.get_args(elem_t)
                if list_args:
                    if len(list_args) != 1:
                        raise ValueError("List must have one type argument")
                    elem_t = list_args[0]
                    _, elem_is_opaque = _unwrap_opaque(elem_t)
                    if elem_is_opaque:
                        return FieldType(
                            primitive=PrimitiveType.ANY,
                            repetition=Repetition.OPTIONAL_REPEATED,
                            coder=None,
                        )
                    # dict elements in optional lists → ANY
                    if _is_dict_like(elem_t) or typing.get_origin(elem_t) is dict:
                        return FieldType(
                            primitive=PrimitiveType.ANY,
                            repetition=Repetition.OPTIONAL_REPEATED,
                            coder=None,
                        )
                    inner_origin = typing.get_origin(elem_t)
                    if inner_origin is not None:
                        raise ValueError(
                            f"List cannot have nested type {_type_name(inner_origin)}"
                        )
                else:
                    elem_t = Any
                repetition = Repetition.OPTIONAL_REPEATED
            elif nested_t is dict or _is_dict_like(elem_t):
                # Optional[dict] or Optional[Dict[str, Any]] → optional ANY.
                # nested_t is dict: elem_t is parameterized (e.g. Dict[str, Any]).
                # elem_t is dict: elem_t is bare dict (nested_t is None).
                return FieldType(
                    primitive=PrimitiveType.ANY,
                    repetition=Repetition.OPTIONAL,
                    coder=None,
                )
            elif nested_t is not None:
                raise ValueError(
                    f"Optional cannot have nested type {_type_name(nested_t)}"
                )
            else:
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
        if self.union_variants is not None:
            if value is None:
                if self.repetition is Repetition.OPTIONAL:
                    return None
                raise ValueError("missing value for required union field")
            for variant in _ordered_union_variants(value, self.union_variants):
                if not _union_variant_accepts_value(value, variant):
                    continue
                try:
                    return variant.normalize(value)
                except (TypeError, ValueError):
                    pass
            raise ValueError(f"failed to normalize value as {self.python_type_name()}")

        if self.repetition is Repetition.REQUIRED:
            return self.primitive.normalize(value)
        elif self.repetition is Repetition.OPTIONAL:
            return None if value is None else self.primitive.normalize(value)
        elif self.repetition is Repetition.REPEATED:
            return [self.primitive.normalize(v) for v in value]
        elif self.repetition is Repetition.OPTIONAL_REPEATED:
            return (
                None if value is None else [self.primitive.normalize(v) for v in value]
            )
        return value

    def json_type(self) -> Dict[str, Any]:
        """Get the JSON Schema type for this field."""
        if self.union_variants is not None:
            jt: Dict[str, Any] = {
                "anyOf": [variant.json_type() for variant in self.union_variants]
            }
            if self.repetition is Repetition.OPTIONAL:
                jt["nullable"] = True
            return jt

        if self.repetition in (Repetition.REPEATED, Repetition.OPTIONAL_REPEATED):
            return {"type": "array", "items": self.primitive.json_type()}
        return self.primitive.json_type()

    def json_encode(self, value: Any) -> Any:
        """Encode a value for JSON serialization."""
        f: Callable[[Any], Any] = self.primitive.json_encode
        if self.primitive is PrimitiveType.CUSTOM:
            assert self.coder is not None
            f = self.coder.encode
        if self.repetition in (Repetition.REPEATED, Repetition.OPTIONAL_REPEATED):
            if value is None:
                return None
            return [f(x) for x in value]
        return f(value)

    def json_decode(self, value: Any) -> Any:
        """Decode a value from JSON."""
        if self.primitive is not PrimitiveType.CUSTOM:
            return value
        assert self.coder is not None
        f = self.coder.decode
        if self.repetition in (Repetition.REPEATED, Repetition.OPTIONAL_REPEATED):
            if value is None:
                return None
            return [f(x) for x in value]
        return f(value)

    def python_type_name(self) -> str:
        """Get the Python type name for this field."""
        if self.union_variants is not None:
            name = " | ".join(
                variant.python_type_name() for variant in self.union_variants
            )
            if self.repetition is Repetition.OPTIONAL:
                return f"Optional[{name}]"
            return name

        if self.repetition is Repetition.REQUIRED:
            return self.primitive.python_type_name()
        elif self.repetition is Repetition.OPTIONAL:
            return f"Optional[{self.primitive.python_type_name()}]"
        elif self.repetition is Repetition.REPEATED:
            return f"List[{self.primitive.python_type_name()}]"
        elif self.repetition is Repetition.OPTIONAL_REPEATED:
            return f"Optional[List[{self.primitive.python_type_name()}]]"
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
            required = []
            for name, field_type in self.fields.items():
                props[name] = field_type.json_type()
                if field_type.repetition in (
                    Repetition.OPTIONAL,
                    Repetition.OPTIONAL_REPEATED,
                ):
                    props[name]["nullable"] = True
                else:
                    required.append(name)
                props[name]["title"] = name.replace("_", " ").title()
            jt.update({"type": "object", "properties": props})
            if required:
                jt["required"] = required

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
                    if ft.repetition not in (
                        Repetition.OPTIONAL,
                        Repetition.OPTIONAL_REPEATED,
                    ):
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
