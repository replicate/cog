import importlib
import inspect
import re
import typing
import warnings
from dataclasses import MISSING, Field
from enum import Enum
from types import ModuleType
from typing import Any, AsyncIterator, Callable, Dict, Iterator, Optional, Type

from coglet import adt, api, asts
from coglet.util import type_name


def _check_parent(child: type, parent: type) -> bool:
    return any(c is parent for c in inspect.getmro(child))


def _validate_setup(f: Callable) -> None:
    assert inspect.isfunction(f), 'setup is not a function'
    spec = inspect.getfullargspec(f)

    assert spec.args[:1] == ['self'], "setup() must have 'self' first argument"
    non_default_parameter_args = spec.args
    if spec.defaults is not None:
        non_default_parameter_args = non_default_parameter_args[: -len(spec.defaults)]
    extra_args = ', '.join(
        [a for a in non_default_parameter_args if a not in {'self', 'weights'}]
    )
    assert extra_args == '', f'unexpected setup() arguments: {extra_args}'
    assert spec.varargs is None, 'setup() must not have *args'
    assert spec.varkw is None, 'setup() must not have **kwargs'
    assert spec.kwonlyargs == [], 'setup() must not have keyword-only args'
    assert spec.kwonlydefaults is None, 'setup() must not have keyword-only defaults'
    assert spec.annotations.get('return') is None, 'setup() must return None'


def _validate_predict(f: Callable, f_name: str, is_class_fn: bool) -> None:
    assert inspect.isfunction(f), f'{f_name} is not a function'
    spec = inspect.getfullargspec(f)

    if is_class_fn:
        assert spec.args[:1] == ['self'], f"{f_name}() must have 'self' first argument"
    assert spec.varargs is None, f'{f_name}() must not have *args'
    assert spec.varkw is None, f'{f_name}() must not have **kwargs'
    assert spec.kwonlyargs == [], f'{f_name}() must not have keyword-only args'
    assert spec.kwonlydefaults is None, (
        f'{f_name}() must not have keyword-only defaults'
    )
    assert spec.annotations.get('return') is not None, (
        f'{f_name}() must not return None'
    )


def _validate_input(name: str, ft: adt.FieldType, cog_in: api.FieldInfo) -> None:
    cog_t = ft.primitive
    in_repr = f'{name}: {ft.python_type()}'
    defaults = []
    def_repr = ''
    if cog_in.default is not None:
        # Handle dataclass fields by extracting the actual default value
        if isinstance(cog_in.default, Field):
            if cog_in.default.default_factory is not MISSING:
                # For default_factory, get a sample value for validation
                actual_default = cog_in.default.default_factory()
            elif cog_in.default.default is not MISSING:
                actual_default = cog_in.default.default
            else:
                actual_default = None
        else:
            actual_default = cog_in.default

        if actual_default is not None:
            if ft.repetition is adt.Repetition.REPEATED:
                defaults = ft.normalize(actual_default)
                def_repr = repr(defaults)
            else:
                defaults = [ft.normalize(actual_default)]
                def_repr = repr(defaults[0])
        else:
            defaults = []
            def_repr = 'None'

    numeric_types = {adt.PrimitiveType.FLOAT, adt.PrimitiveType.INTEGER}
    if cog_in.ge is not None or cog_in.le is not None:
        assert cog_t in numeric_types, f'incompatible input type for ge/le: {in_repr}'
        if defaults:
            if cog_in.ge is not None:
                assert all(x >= cog_in.ge for x in defaults), (
                    f'default={def_repr} conflicts with ge={cog_in.ge} for input: {in_repr}'
                )
            if cog_in.le is not None:
                assert all(x <= cog_in.le for x in defaults), (
                    f'default={def_repr} conflicts with le={cog_in.le} for input: {in_repr}'
                )

    if cog_in.min_length is not None or cog_in.max_length is not None:
        assert cog_t is adt.PrimitiveType.STRING, (
            f'incompatible input type for min_length/max_length: {in_repr}'
        )
        if defaults:
            if cog_in.min_length is not None:
                assert all(len(x) >= cog_in.min_length for x in defaults), (
                    f'default={def_repr} conflicts with min_length={cog_in.min_length} for input: {in_repr}'
                )
            if cog_in.max_length is not None:
                assert all(len(x) <= cog_in.max_length for x in defaults), (
                    f'default={def_repr} conflicts with max_length={cog_in.max_length} for input: {in_repr}'
                )

    if cog_in.regex is not None:
        assert cog_t is adt.PrimitiveType.STRING, (
            f'incompatible input type for regex: {in_repr}'
        )
        if defaults:
            regex = re.compile(cog_in.regex)
            assert all(regex.match(x) for x in defaults), (
                f'default={def_repr} not a regex match for input: {in_repr}'
            )

    choice_types = {adt.PrimitiveType.INTEGER, adt.PrimitiveType.STRING}
    if cog_in.choices is not None:
        assert cog_t in choice_types, f'incompatible input type for choices: {in_repr}'
        assert len(cog_in.choices) >= 2, (
            f'choices={repr(cog_in.choices)} must have >= 2 elements: {in_repr}'
        )
        assert cog_in.ge is None and cog_in.le is None, (
            f'choices and ge/le are mutually exclusive: {in_repr}'
        )
        assert cog_in.min_length is None and cog_in.max_length is None, (
            f'choices and min_length/max_length are mutually exclusive: {in_repr}'
        )
        # Normalize x: str=Input(choices=[enum_t.A, enum_t.B, ...]) before checking types
        choices = []
        for c in cog_in.choices:
            if isinstance(c, Enum):
                c = cog_t.normalize(c)
            choices.append(c)
        assert all(adt.PrimitiveType.from_type(type(x)) is cog_t for x in choices), (
            f'not all choices have the same type as input: {in_repr}'
        )


def _input_adt(
    order: int, name: str, tpe: type, cog_in: Optional[api.FieldInfo]
) -> adt.Input:
    try:
        ft = adt.FieldType.from_type(tpe)
    except AssertionError as e:
        raise AssertionError(f'invalid input field {name}: {e}')
    if cog_in is None:
        return adt.Input(
            name=name,
            order=order,
            type=ft,
        )
    else:
        _validate_input(name, ft, cog_in)

        # Handle dataclass fields properly
        if isinstance(cog_in.default, Field):
            # This is a dataclass field, extract the default value or factory
            if cog_in.default.default_factory is not MISSING:
                # This field uses default_factory, store the field itself
                default = cog_in.default
            elif cog_in.default.default is not MISSING:
                # Has a regular default value
                default = ft.normalize(cog_in.default.default)
            else:
                # No default
                default = None
        else:
            default = None if cog_in.default is None else ft.normalize(cog_in.default)
        choices = (
            None
            if cog_in.choices is None
            else [ft.primitive.normalize(c) for c in cog_in.choices]
        )
        return adt.Input(
            name=name,
            order=order,
            type=ft,
            default=default,
            description=cog_in.description,
            ge=float(cog_in.ge) if cog_in.ge is not None else None,
            le=float(cog_in.le) if cog_in.le is not None else None,
            min_length=cog_in.min_length,
            max_length=cog_in.max_length,
            regex=cog_in.regex,
            choices=choices,
            deprecated=cog_in.deprecated,
        )


# Mimic PrimitiveType behavior to support Any output type
class AnyType:
    @staticmethod
    def normalize(value: Any) -> Any:
        return value

    @staticmethod
    def json_type() -> dict[str, Any]:
        # Compat: legacy Cog does not even add {"type": "object"}
        return {}

    @staticmethod
    def json_encode(value: Any) -> Any:
        return value


_any_type = AnyType()


def _output_adt(tpe: type) -> adt.Output:
    if tpe is Any:
        print(
            'Warning: use of Any as output type is error prone and highly-discouraged'
        )
        return adt.Output(kind=adt.Kind.SINGLE, type=_any_type)  # type: ignore
    if inspect.isclass(tpe) and _check_parent(tpe, api.BaseModel):
        fields = {}
        for name, t in tpe.__annotations__.items():
            ft = adt.FieldType.from_type(t)
            fields[name] = ft
        return adt.Output(kind=adt.Kind.OBJECT, fields=fields)

    origin = typing.get_origin(tpe)
    kind = None
    concat_iters = {api.ConcatenateIterator, api.AsyncConcatenateIterator}
    if origin in {typing.get_origin(Iterator), typing.get_origin(AsyncIterator)}:
        kind = adt.Kind.ITERATOR
        t_args = typing.get_args(tpe)
        assert len(t_args) == 1, 'iterator type must have a type argument'
        ft = adt.FieldType.from_type(t_args[0])
        assert ft.repetition is adt.Repetition.REQUIRED
    # origin is None if type argument is missing
    elif origin in concat_iters or tpe in concat_iters:
        kind = adt.Kind.CONCAT_ITERATOR
        t_args = typing.get_args(tpe)
        assert len(t_args) == 1, 'iterator type must have a type argument'
        ft = adt.FieldType.from_type(t_args[0])
        assert ft.repetition is adt.Repetition.REQUIRED
        assert ft.primitive is adt.PrimitiveType.STRING, (
            f'{type_name(tpe)} must have str element'
        )
    else:
        ft = adt.FieldType.from_type(tpe)
        assert ft.repetition is not adt.Repetition.OPTIONAL, (
            'output must not be Optional'
        )
        if ft.repetition == adt.Repetition.REQUIRED:
            kind = adt.Kind.SINGLE
        elif ft.repetition == adt.Repetition.REPEATED:
            kind = adt.Kind.LIST
    assert kind is not None
    return adt.Output(kind=kind, type=ft.primitive, coder=ft.coder)


def _predictor_adt(
    module_name: str, predictor_name: str, f: Callable, f_name: str, is_class_fn: bool
) -> adt.Predictor:
    _validate_predict(f, f_name, is_class_fn)
    spec = inspect.getfullargspec(f)
    # 1st argument is self for class fn
    names = spec.args[1:] if is_class_fn else spec.args
    defaults = spec.defaults if spec.defaults is not None else []
    cog_ins = [None] * (len(names) - len(defaults)) + list(defaults)
    inputs = {}
    for i, (name, cog_in) in enumerate(zip(names, cog_ins)):
        tpe = spec.annotations.get(name)
        assert tpe is not None, f'missing type annotation for input: {name}'
        inputs[name] = _input_adt(i, name, tpe, cog_in)
    output = _output_adt(spec.annotations['return'])
    return adt.Predictor(module_name, predictor_name, inputs, output)


# setup and predict might be decorated
def _unwrap(f: Callable) -> Callable:
    g = f
    while hasattr(g, '__closure__') and g.__closure__ is not None:
        cs = [
            c.cell_contents
            for c in g.__closure__
            if inspect.isfunction(c.cell_contents)
        ]
        assert len(cs) <= 1, f'unable to inspect function decorator: {f}'
        if len(cs) == 0:
            # No more functions in closure
            return g
        else:
            # 1 function in closure, keep digging
            g = cs[0]
    return g


def check_input(
    adt_ins: Dict[str, adt.Input], inputs: Dict[str, Any]
) -> Dict[str, Any]:
    kwargs: Dict[str, Any] = {}
    for name, value in inputs.items():
        # assert name in adt_ins, f'unknown input field: {name}'
        adt_in = adt_ins.get(name)
        if adt_in is None:
            print(f'WARNING unknown input field ignored: {name}')
        else:
            kwargs[name] = adt_in.type.normalize(value)
    for name, adt_in in adt_ins.items():
        if name not in kwargs:
            # Handle dataclass fields properly
            if isinstance(adt_in.default, Field):
                # This is a dataclass field with default_factory
                if adt_in.default.default_factory is not MISSING:
                    kwargs[name] = adt_in.default.default_factory()
                elif adt_in.default.default is not MISSING:
                    kwargs[name] = adt_in.default.default
                else:
                    # No default or factory
                    if adt_in.type.repetition is not adt.Repetition.OPTIONAL:
                        assert False, f'missing required input field: {name}'
                    kwargs[name] = None
            elif adt_in.default is not None:
                kwargs[name] = adt_in.default
            else:
                # default=None is only allowed on `Optional[<type>]`
                if adt_in.type.repetition is not adt.Repetition.OPTIONAL:
                    assert False, f'missing required input field: {name}'
                kwargs[name] = None

        values = []
        if adt_in.type.repetition is adt.Repetition.REQUIRED:
            values = [kwargs[name]]
        elif adt_in.type.repetition is adt.Repetition.OPTIONAL:
            # Optional[<type>] and default to None, skip validation
            if kwargs[name] is None:
                values = []
            else:
                values = [kwargs[name]]
        elif adt_in.type.repetition is adt.Repetition.REPEATED:
            values = kwargs[name]
        v = kwargs[name]
        if adt_in.ge is not None:
            assert all(x >= adt_in.ge for x in values), (
                f'invalid input value: {name}={repr(v)} fails constraint >= {adt_in.ge}'
            )
        if adt_in.le is not None:
            assert all(x <= adt_in.le for x in values), (
                f'invalid input value: {name}={repr(v)} fails constraint <= {adt_in.le}'
            )
        if adt_in.min_length is not None:
            assert all(len(x) >= adt_in.min_length for x in values), (
                f'invalid input value: {name}={repr(v)} fails constraint len() >= {adt_in.min_length}'
            )
        if adt_in.max_length is not None:
            assert all(len(x) <= adt_in.max_length for x in values), (
                f'invalid input value: {name}={repr(v)} fails constraint len() <= {adt_in.max_length}'
            )
        if adt_in.regex is not None:
            p = re.compile(adt_in.regex)
            assert all(p.match(x) is not None for x in values), (
                f'invalid input value: {name}={repr(v)} does not match regex {repr(adt_in.regex)}'
            )
        if adt_in.choices is not None:
            assert all(x in adt_in.choices for x in values), (
                f'invalid input value: {name}={repr(v)} does not match choices {repr(adt_in.choices)}'
            )
    return kwargs


def _is_coder(cls: Type) -> bool:
    return (
        inspect.isclass(cls) and cls is not api.Coder and _check_parent(cls, api.Coder)
    )


def _find_coders(module: ModuleType) -> None:
    found = False
    # from cog.coders.some_coders import SomeCoder
    for _, c in inspect.getmembers(module, _is_coder):
        warnings.warn(f'Registering coder: {c}')
        found = True
        api.Coder.register(c)
    # from cog.coders import some_coders
    for _, m in inspect.getmembers(module, inspect.ismodule):
        for _, c in inspect.getmembers(m, _is_coder):
            warnings.warn(f'Registering coder: {c}')
            found = True
            api.Coder.register(c)
    if found:
        warnings.warn(
            'Coders are experimental and might change or be removed without warning.'
        )


def create_predictor(
    module_name: str, predictor_name: str, inspect_ast: bool = True
) -> adt.Predictor:
    module = importlib.import_module(module_name)

    # "from __future__ import annotations" turns all type annotations from actual types to strings
    # and break all inspection logic
    import __future__

    if (
        hasattr(module, 'annotations')
        and getattr(module, 'annotations') == __future__.annotations
    ):
        raise AssertionError(
            'predictor with "from __future__ import annotations" is not supported'
        )

    fullname = f'{module_name}.{predictor_name}'
    assert hasattr(module, predictor_name), f'predictor not found: {fullname}'
    p = getattr(module, predictor_name)
    if inspect.isclass(p):
        assert _check_parent(p, api.BasePredictor), (
            f'predictor {fullname} does not inherit cog.BasePredictor'
        )

        assert hasattr(p, 'setup'), f'setup method not found: {fullname}'
        assert hasattr(p, 'predict'), f'predict method not found: {fullname}'
        _validate_setup(_unwrap(getattr(p, 'setup')))
        predict_fn_name = 'predict'
        predict_fn = _unwrap(getattr(p, predict_fn_name))
        is_class_fn = True
    elif inspect.isfunction(p):
        predict_fn_name = predictor_name
        predict_fn = _unwrap(p)
        is_class_fn = False
    else:
        raise AssertionError(f'invalid predictor {fullname}')

    # Find coders users by module before validating predict function
    _find_coders(module)

    predictor = _predictor_adt(
        module_name, predictor_name, predict_fn, predict_fn_name, is_class_fn
    )

    # AST checks at the end after all other checks pass
    # Only check when running from cog.command.openapi_schema -> coglet.schema
    # So that old models that violate this check can still run
    if inspect_ast and module.__file__ is not None:
        method = 'predict' if is_class_fn else predictor_name
        asts.inspect(module.__file__, method)

    return predictor


def get_test_inputs(
    p: api.BasePredictor, inputs: Dict[str, adt.Input]
) -> Dict[str, Any]:
    if hasattr(p, 'test_inputs'):
        test_inputs = getattr(p, 'test_inputs')
    else:
        # Fall back to defaults if no test_inputs is defined
        test_inputs = {}

    try:
        return check_input(inputs, test_inputs)
    except AssertionError as e:
        raise AssertionError(f'invalid test_inputs: {e}')
