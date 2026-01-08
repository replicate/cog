import dataclasses
import os
import sys
import typing
from dataclasses import dataclass
from enum import Enum, auto
from typing import Any, Callable, Dict, List, Optional, Set, Union

from cog.coder import dataclass_coder, json_coder, set_coder
from coglet import api
from coglet.util import type_name


def _is_union(tpe: type) -> bool:
    if typing.get_origin(tpe) is Union:
        return True
    if sys.version_info >= (3, 10):
        from types import UnionType

        if typing.get_origin(tpe) is UnionType:
            return True
    return False


class PrimitiveType(Enum):
    BOOL = auto()
    FLOAT = auto()
    INTEGER = auto()
    STRING = auto()
    PATH = auto()
    SECRET = auto()
    ANY = auto()
    CUSTOM = auto()

    @staticmethod
    def _python_type() -> dict:
        return {
            PrimitiveType.BOOL: bool,
            PrimitiveType.FLOAT: float,
            PrimitiveType.INTEGER: int,
            PrimitiveType.STRING: str,
            PrimitiveType.PATH: api.Path,
            PrimitiveType.SECRET: api.Secret,
            PrimitiveType.ANY: Any,
            PrimitiveType.CUSTOM: Any,
        }

    @staticmethod
    def _json_type() -> dict:
        return {
            PrimitiveType.BOOL: 'boolean',
            PrimitiveType.FLOAT: 'number',
            PrimitiveType.INTEGER: 'integer',
            PrimitiveType.STRING: 'string',
            PrimitiveType.PATH: 'string',
            PrimitiveType.SECRET: 'string',
            PrimitiveType.ANY: 'object',
            PrimitiveType.CUSTOM: 'object',
        }

    @staticmethod
    def _adt_type() -> dict:
        return {
            bool: PrimitiveType.BOOL,
            float: PrimitiveType.FLOAT,
            int: PrimitiveType.INTEGER,
            str: PrimitiveType.STRING,
            api.Path: PrimitiveType.PATH,
            api.Secret: PrimitiveType.SECRET,
            Any: PrimitiveType.ANY,
        }

    @staticmethod
    def from_type(tpe: type) -> Any:
        if match := PrimitiveType._adt_type().get(tpe):
            return match

        try:
            if tpe is os.PathLike or issubclass(tpe, os.PathLike):
                return PrimitiveType.PATH
        except TypeError:
            # Catch arg 1 is not a class in issubclass
            pass

        return PrimitiveType.CUSTOM

    def normalize(self, value: Any) -> Any:
        pt = PrimitiveType._python_type()[self]
        tpe = type(value)
        if self is PrimitiveType.CUSTOM:
            # Custom type, leave as is
            return value
        elif self is PrimitiveType.ANY:
            # Any type, accept any value as-is
            return value
        elif self in {self.PATH, self.SECRET}:
            # String-ly types, only upcast
            return value if tpe is pt else pt(value)
        else:
            if issubclass(tpe, Enum):
                assert issubclass(tpe, pt), (
                    f'enum {type_name(tpe)} is used as {type_name(pt)} but does not extend it'
                )
                value = value.value
            v = pt(value)
            assert v == value, f'failed to normalize value {value} as {type_name(pt)}'
            return v

    def python_type(self) -> str:
        return type_name(PrimitiveType._python_type()[self])

    def json_type(self) -> dict[str, Any]:
        jt: dict[str, Any] = {'type': self._json_type()[self]}
        if self is self.PATH:
            jt['format'] = 'uri'
        elif self is self.SECRET:
            jt['format'] = 'password'
            jt['writeOnly'] = True
            jt['x-cog-secret'] = True
        return jt

    def json_encode(self, value: Any) -> Any:
        if self is self.FLOAT:
            return float(value)
        elif self in {self.PATH, self.SECRET}:
            # Leave these as is and let the file runner handle special encoding
            return value
        elif self is self.ANY:
            # Any type, return as-is
            return value
        else:
            return value


class Repetition(Enum):
    REQUIRED = 1
    OPTIONAL = 2
    REPEATED = 3


@dataclass(frozen=True)
class FieldType:
    primitive: PrimitiveType
    repetition: Repetition
    coder: Optional[api.Coder]

    @staticmethod
    def from_type(tpe: type):
        origin = typing.get_origin(tpe)

        # Handle bare collection types first
        if tpe is list:
            # Bare list -> List[Any]
            tpe = List[Any]
            origin = typing.get_origin(tpe)
        elif tpe is dict:
            # Bare dict -> Dict[str, Any]
            tpe = Dict[str, Any]
            origin = typing.get_origin(tpe)
        elif tpe is set:
            # Bare set -> Set[Any]
            tpe = Set[Any]
            origin = typing.get_origin(tpe)

        if origin in (list, List):
            t_args = typing.get_args(tpe)
            if t_args:
                assert len(t_args) == 1, 'List must have one type argument'
                elem_t = t_args[0]
                # Fail fast to avoid the cryptic "unsupported Cog type" error later with elem_t
                nested_t = typing.get_origin(elem_t)
                assert nested_t is None, (
                    f'List cannot have nested type {type_name(nested_t)}'
                )
            else:
                # Bare list type without type arguments, treat as List[Any]
                elem_t = Any
            repetition = Repetition.REPEATED
        elif _is_union(tpe):
            t_args = typing.get_args(tpe)
            assert len(t_args) == 2 and type(None) in t_args, (
                f'unsupported union type {tpe}'
            )
            elem_t = t_args[0] if t_args[1] is type(None) else t_args[1]
            # Fail fast to avoid the cryptic "unsupported Cog type" error later with elem_t
            nested_t = typing.get_origin(elem_t)
            assert nested_t is None, (
                f'Optional cannot have nested type {type_name(nested_t)}'
            )
            repetition = Repetition.OPTIONAL
        else:
            elem_t = tpe
            repetition = Repetition.REQUIRED
        cog_t = PrimitiveType.from_type(elem_t)
        coder = None
        if cog_t is PrimitiveType.CUSTOM:
            api.Coder.register(dataclass_coder.DataclassCoder)
            api.Coder.register(json_coder.JsonCoder)
            api.Coder.register(set_coder.SetCoder)
            coder = api.Coder.lookup(elem_t)
            assert coder is not None, f'unsupported Cog type {type_name(elem_t)}'

        return FieldType(primitive=cog_t, repetition=repetition, coder=coder)

    def normalize(self, value: Any) -> Any:
        if self.repetition is Repetition.REQUIRED:
            return self.primitive.normalize(value)
        elif self.repetition is Repetition.OPTIONAL:
            return None if value is None else self.primitive.normalize(value)
        elif self.repetition is Repetition.REPEATED:
            return [self.primitive.normalize(v) for v in value]
        else:
            # Should not reach here
            return value

    def json_type(self) -> dict[str, Any]:
        if self.repetition is Repetition.REPEATED:
            return {'type': 'array', 'items': self.primitive.json_type()}
        else:
            return self.primitive.json_type()

    def json_encode(self, value: Any) -> Any:
        f: Callable[[Any], Any] = self.primitive.json_encode
        if self.primitive is PrimitiveType.CUSTOM:
            assert self.coder is not None
            f = self.coder.encode
        if self.repetition is Repetition.REPEATED:
            return [f(x) for x in value]
        else:
            return f(value)

    def json_decode(self, value: Any) -> Any:
        if self.primitive is not PrimitiveType.CUSTOM:
            return value
        assert self.coder is not None
        f = self.coder.decode
        if self.repetition is Repetition.REPEATED:
            return [f(x) for x in value]
        else:
            return f(value)

    def python_type(self) -> str:
        if self.repetition is Repetition.REQUIRED:
            return self.primitive.python_type()
        elif self.repetition is Repetition.OPTIONAL:
            return f'Optional[{self.primitive.python_type()}]'
        elif self.repetition is Repetition.REPEATED:
            return f'List[{self.primitive.python_type()}]'
        else:
            # Should not reach here
            return self.primitive.python_type()


@dataclass(frozen=True)
class Input:
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


class Kind(Enum):
    SINGLE = 1
    LIST = 2
    ITERATOR = 3
    CONCAT_ITERATOR = 4
    OBJECT = 5


@dataclass(frozen=True)
class Output:
    kind: Kind
    type: Optional[PrimitiveType] = None
    fields: Optional[Dict[str, FieldType]] = None
    coder: Optional[api.Coder] = None

    def json_type(self) -> dict[str, Any]:
        jt: dict[str, Any] = {'title': 'Output'}
        if self.kind is Kind.SINGLE:
            assert self.type is not None
            jt.update(self.type.json_type())
        elif self.kind is Kind.LIST:
            assert self.type is not None
            jt.update({'type': 'array', 'items': self.type.json_type()})
        elif self.kind is Kind.ITERATOR:
            assert self.type is not None
            jt.update(
                {
                    'type': 'array',
                    'items': self.type.json_type(),
                    'x-cog-array-type': 'iterator',
                }
            )
        elif self.kind is Kind.CONCAT_ITERATOR:
            assert self.type is not None
            jt.update(
                {
                    'type': 'array',
                    'items': self.type.json_type(),
                    'x-cog-array-type': 'iterator',
                    'x-cog-array-display': 'concatenate',
                }
            )
        elif self.kind is Kind.OBJECT:
            assert self.fields is not None
            props = {}
            for name, cog_t in self.fields.items():
                props[name] = cog_t.primitive.json_type()
                props[name]['title'] = name.replace('_', ' ').title()
            jt.update(
                {
                    'type': 'object',
                    'properties': props,
                    'required': list(self.fields.keys()),
                }
            )
        return jt

    def _transform(self, value: Any, json: bool) -> Any:
        if self.kind in {Kind.SINGLE, Kind.ITERATOR, Kind.CONCAT_ITERATOR}:
            assert self.type is not None
            f: Callable[[Any], Any] = (
                self.type.json_encode if json else self.type.normalize
            )
            return f(value)
        elif self.kind is Kind.LIST:
            assert self.type is not None
            f = self.type.json_encode if json else self.type.normalize
            return [f(x) for x in value]
        elif self.kind is Kind.OBJECT:
            assert self.fields is not None
            for name, ft in self.fields.items():
                f = ft.json_encode if json else ft.normalize
                assert hasattr(value, name), f'missing output field: {name} {value}'
                v = getattr(value, name)
                if v is None:
                    assert ft.repetition is Repetition.OPTIONAL, (
                        f'missing value for output field: {name}'
                    )
                setattr(value, name, f(v))
            return value
        raise RuntimeError(f'unsupported output kind {self.kind}')

    def normalize(self, value: Any) -> Any:
        return self._transform(value, json=False)

    def json_encode(self, value: Any) -> Any:
        if self.coder is not None:
            if self.kind is Kind.LIST:
                return [self.coder.encode(x) for x in value]
            else:
                return self.coder.encode(value)
        o = self._transform(value, json=True)
        if self.kind is Kind.OBJECT:
            # Further expand Output into dict
            tpe = type(o)
            assert dataclasses.is_dataclass(tpe), f'{tpe} is not a dataclass'
            r = {}
            for f in dataclasses.fields(o):
                r[f.name] = getattr(o, f.name)
            return r
        else:
            return o


@dataclass(frozen=True)
class Predictor:
    module_name: str
    predictor_name: str  # class or function
    inputs: Dict[str, Input]
    output: Output
