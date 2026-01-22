"""
Internal inspector for predictor introspection.

This module provides functions to inspect predictor classes and functions,
extract input/output type information, and validate inputs.
"""

import importlib
import inspect
import re
import sys
import typing
import warnings
from dataclasses import MISSING, Field
from enum import Enum
from types import ModuleType
from typing import Any, AsyncIterator, Callable, Dict, Iterator, Optional, Type

from . import _adt as adt
from .coder import Coder
from .input import FieldInfo
from .model import BaseModel
from .predictor import BasePredictor
from .types import AsyncConcatenateIterator, ConcatenateIterator


def _check_parent(child: type, parent: type) -> bool:
    """Check if a type has a parent in its MRO."""
    return any(c is parent for c in inspect.getmro(child))


def _type_name(tpe: Any) -> str:
    """Get a human-readable name for a type."""
    try:
        return tpe.__name__
    except AttributeError:
        return str(tpe)


def _validate_setup(f: Callable[..., Any]) -> None:
    """Validate a predictor's setup method."""
    if not inspect.isfunction(f):
        raise ValueError("setup is not a function")

    spec = inspect.getfullargspec(f)

    if spec.args[:1] != ["self"]:
        raise ValueError("setup() must have 'self' as first argument")

    non_default_args = spec.args
    if spec.defaults is not None:
        non_default_args = non_default_args[: -len(spec.defaults)]

    extra_args = [a for a in non_default_args if a not in {"self", "weights"}]
    if extra_args:
        raise ValueError(f"unexpected setup() arguments: {', '.join(extra_args)}")

    if spec.varargs is not None:
        raise ValueError("setup() must not have *args")
    if spec.varkw is not None:
        raise ValueError("setup() must not have **kwargs")
    if spec.kwonlyargs:
        raise ValueError("setup() must not have keyword-only args")
    if spec.kwonlydefaults:
        raise ValueError("setup() must not have keyword-only defaults")
    if spec.annotations.get("return") is not None:
        raise ValueError("setup() must return None")


def _validate_predict(f: Callable[..., Any], f_name: str, is_class_fn: bool) -> None:
    """Validate a predictor's predict method."""
    if not inspect.isfunction(f):
        raise ValueError(f"{f_name} is not a function")

    spec = inspect.getfullargspec(f)

    if is_class_fn and spec.args[:1] != ["self"]:
        raise ValueError(f"{f_name}() must have 'self' as first argument")
    if spec.varargs is not None:
        raise ValueError(f"{f_name}() must not have *args")
    if spec.varkw is not None:
        raise ValueError(f"{f_name}() must not have **kwargs")
    if spec.kwonlyargs:
        raise ValueError(f"{f_name}() must not have keyword-only args")
    if spec.kwonlydefaults:
        raise ValueError(f"{f_name}() must not have keyword-only defaults")
    if spec.annotations.get("return") is None:
        raise ValueError(f"{f_name}() must have a return type annotation")


def _validate_input_constraints(
    name: str, ft: adt.FieldType, field_info: FieldInfo
) -> None:
    """Validate that FieldInfo constraints are compatible with the field type."""
    cog_t = ft.primitive
    in_repr = f"{name}: {ft.python_type_name()}"

    # Extract actual default for validation
    defaults = []
    if field_info.default is not None:
        if isinstance(field_info.default, Field):
            if field_info.default.default_factory is not MISSING:
                actual_default = field_info.default.default_factory()
            elif field_info.default.default is not MISSING:
                actual_default = field_info.default.default
            else:
                actual_default = None
        else:
            actual_default = field_info.default

        if actual_default is not None:
            if ft.repetition is adt.Repetition.REPEATED:
                defaults = ft.normalize(actual_default)
            else:
                defaults = [ft.normalize(actual_default)]

    numeric_types = {adt.PrimitiveType.FLOAT, adt.PrimitiveType.INTEGER}

    # Validate ge/le constraints
    if field_info.ge is not None or field_info.le is not None:
        if cog_t not in numeric_types:
            raise ValueError(f"incompatible input type for ge/le: {in_repr}")
        if defaults:
            if field_info.ge is not None and not all(
                x >= field_info.ge for x in defaults
            ):
                raise ValueError(
                    f"invalid default for {in_repr}: must be at minimum {field_info.ge}"
                )
            if field_info.le is not None and not all(
                x <= field_info.le for x in defaults
            ):
                raise ValueError(
                    f"invalid default for {in_repr}: must be at maximum {field_info.le}"
                )

    # Validate min_length/max_length constraints
    if field_info.min_length is not None or field_info.max_length is not None:
        if cog_t is not adt.PrimitiveType.STRING:
            raise ValueError(
                f"incompatible input type for min_length/max_length: {in_repr}"
            )
        if defaults:
            if field_info.min_length is not None and not all(
                len(x) >= field_info.min_length for x in defaults
            ):
                raise ValueError(
                    f"default conflicts with min_length={field_info.min_length} for input: {in_repr}"
                )
            if field_info.max_length is not None and not all(
                len(x) <= field_info.max_length for x in defaults
            ):
                raise ValueError(
                    f"default conflicts with max_length={field_info.max_length} for input: {in_repr}"
                )

    # Validate regex constraint
    if field_info.regex is not None:
        if cog_t is not adt.PrimitiveType.STRING:
            raise ValueError(f"incompatible input type for regex: {in_repr}")
        if defaults:
            regex = re.compile(field_info.regex)
            if not all(regex.match(x) for x in defaults):
                raise ValueError(f"default not a regex match for input: {in_repr}")

    # Validate choices constraint
    if field_info.choices is not None:
        choice_types = {adt.PrimitiveType.INTEGER, adt.PrimitiveType.STRING}
        if cog_t not in choice_types:
            raise ValueError(f"incompatible input type for choices: {in_repr}")
        if len(field_info.choices) < 2:
            raise ValueError(
                f"choices={field_info.choices!r} must have >= 2 elements: {in_repr}"
            )
        if field_info.ge is not None or field_info.le is not None:
            raise ValueError(f"choices and ge/le are mutually exclusive: {in_repr}")
        if field_info.min_length is not None or field_info.max_length is not None:
            raise ValueError(
                f"choices and min_length/max_length are mutually exclusive: {in_repr}"
            )
        # Normalize enum values in choices
        choices = [
            cog_t.normalize(c) if isinstance(c, Enum) else c for c in field_info.choices
        ]
        if not all(adt.PrimitiveType.from_type(type(x)) is cog_t for x in choices):
            raise ValueError(f"not all choices have the same type as input: {in_repr}")


def _create_input_field(
    order: int, name: str, tpe: type, field_info: Any
) -> adt.InputField:
    """Create an InputField from type annotation and optional FieldInfo or raw default."""
    try:
        ft = adt.FieldType.from_type(tpe)
    except (ValueError, AssertionError) as e:
        raise ValueError(f"invalid input field {name}: {e}") from e

    if field_info is None:
        return adt.InputField(name=name, order=order, type=ft)

    # Handle raw default values (not FieldInfo)
    if not isinstance(field_info, FieldInfo):
        # It's a raw default value like "world" or 42
        default = ft.normalize(field_info) if field_info is not None else None
        return adt.InputField(name=name, order=order, type=ft, default=default)

    _validate_input_constraints(name, ft, field_info)

    # Extract default value
    if isinstance(field_info.default, Field):
        if field_info.default.default_factory is not MISSING:
            default = field_info.default
        elif field_info.default.default is not MISSING:
            default = ft.normalize(field_info.default.default)
        else:
            default = None
    else:
        default = (
            None if field_info.default is None else ft.normalize(field_info.default)
        )

    # Normalize choices
    choices = (
        None
        if field_info.choices is None
        else [ft.primitive.normalize(c) for c in field_info.choices]
    )

    return adt.InputField(
        name=name,
        order=order,
        type=ft,
        default=default,
        description=field_info.description,
        ge=float(field_info.ge) if field_info.ge is not None else None,
        le=float(field_info.le) if field_info.le is not None else None,
        min_length=field_info.min_length,
        max_length=field_info.max_length,
        regex=field_info.regex,
        choices=choices,
        deprecated=field_info.deprecated,
    )


class _AnyType:
    """Placeholder type for Any output (for compatibility)."""

    @staticmethod
    def normalize(value: Any) -> Any:
        return value

    @staticmethod
    def json_type() -> Dict[str, Any]:
        return {}

    @staticmethod
    def json_encode(value: Any) -> Any:
        return value


_any_type = _AnyType()


def _create_output_type(tpe: type) -> adt.OutputType:
    """Create an OutputType from a return type annotation."""
    if tpe is Any:
        print(
            "Warning: use of Any as output type is error prone and highly-discouraged"
        )
        return adt.OutputType(kind=adt.OutputKind.SINGLE, type=_any_type)  # type: ignore[arg-type]

    if inspect.isclass(tpe) and _check_parent(tpe, BaseModel):
        fields = {}
        for name, t in tpe.__annotations__.items():
            ft = adt.FieldType.from_type(t)
            fields[name] = ft
        return adt.OutputType(kind=adt.OutputKind.OBJECT, fields=fields)

    origin = typing.get_origin(tpe)
    concat_iters = {ConcatenateIterator, AsyncConcatenateIterator}

    if origin in {typing.get_origin(Iterator), typing.get_origin(AsyncIterator)}:
        kind = adt.OutputKind.ITERATOR
        t_args = typing.get_args(tpe)
        if len(t_args) != 1:
            raise ValueError("iterator type must have a type argument")
        ft = adt.FieldType.from_type(t_args[0])
        if ft.repetition is not adt.Repetition.REQUIRED:
            raise ValueError("iterator element type must not be Optional or List")

    elif origin in concat_iters or tpe in concat_iters:
        kind = adt.OutputKind.CONCAT_ITERATOR
        t_args = typing.get_args(tpe)
        if len(t_args) != 1:
            raise ValueError("iterator type must have a type argument")
        ft = adt.FieldType.from_type(t_args[0])
        if ft.repetition is not adt.Repetition.REQUIRED:
            raise ValueError("iterator element type must not be Optional or List")
        if ft.primitive is not adt.PrimitiveType.STRING:
            raise ValueError(f"{_type_name(tpe)} must have str element")

    else:
        ft = adt.FieldType.from_type(tpe)
        if ft.repetition is adt.Repetition.OPTIONAL:
            raise ValueError("output must not be Optional")
        if ft.repetition == adt.Repetition.REQUIRED:
            kind = adt.OutputKind.SINGLE
        elif ft.repetition == adt.Repetition.REPEATED:
            kind = adt.OutputKind.LIST
        else:
            raise RuntimeError(f"unexpected repetition: {ft.repetition}")

    return adt.OutputType(kind=kind, type=ft.primitive, coder=ft.coder)


def _create_predictor_info(
    module_name: str,
    predictor_name: str,
    f: Callable[..., Any],
    f_name: str,
    is_class_fn: bool,
) -> adt.PredictorInfo:
    """Create PredictorInfo from a predict function."""
    _validate_predict(f, f_name, is_class_fn)
    spec = inspect.getfullargspec(f)

    # Use get_type_hints to resolve string annotations (from __future__ import annotations)
    try:
        type_hints = typing.get_type_hints(f)
    except Exception:
        # Fall back to raw annotations if get_type_hints fails
        type_hints = spec.annotations

    # Skip 'self' for class methods
    names = spec.args[1:] if is_class_fn else spec.args
    defaults = list(spec.defaults) if spec.defaults else []
    field_infos = [None] * (len(names) - len(defaults)) + defaults

    inputs: Dict[str, adt.InputField] = {}
    for i, (name, field_info) in enumerate(zip(names, field_infos)):
        tpe = type_hints.get(name)
        if tpe is None:
            raise ValueError(f"missing type annotation for input: {name}")
        inputs[name] = _create_input_field(i, name, tpe, field_info)

    output = _create_output_type(
        type_hints.get("return", spec.annotations.get("return"))
    )
    return adt.PredictorInfo(module_name, predictor_name, inputs, output)


def _unwrap(f: Callable[..., Any]) -> Callable[..., Any]:
    """Unwrap decorated functions to get the original function."""
    g = f
    while hasattr(g, "__closure__") and g.__closure__ is not None:
        cs = [
            c.cell_contents
            for c in g.__closure__
            if inspect.isfunction(c.cell_contents)
        ]
        if len(cs) > 1:
            raise ValueError(f"unable to inspect function decorator: {f}")
        if len(cs) == 0:
            return g
        g = cs[0]
    return g


def _is_coder(cls: Type[Any]) -> bool:
    """Check if a class is a Coder subclass."""
    return inspect.isclass(cls) and cls is not Coder and _check_parent(cls, Coder)


def _find_coders(module: ModuleType) -> None:
    """Find and register coders defined in a module."""
    found = False

    # Direct imports: from cog.coders.some_coder import SomeCoder
    for _, c in inspect.getmembers(module, _is_coder):
        warnings.warn(f"Registering coder: {c}")
        found = True
        Coder.register(c)

    # Module imports: from cog.coders import some_coders
    for _, m in inspect.getmembers(module, inspect.ismodule):
        for _, c in inspect.getmembers(m, _is_coder):
            warnings.warn(f"Registering coder: {c}")
            found = True
            Coder.register(c)

    if found:
        warnings.warn(
            "Coders are experimental and might change or be removed without warning."
        )


def create_predictor(module_name: str, predictor_name: str) -> adt.PredictorInfo:
    """
    Create PredictorInfo by inspecting a predictor class or function.

    Args:
        module_name: The module containing the predictor
        predictor_name: The name of the predictor class or function

    Returns:
        PredictorInfo with input/output type information
    """
    try:
        module = importlib.import_module(module_name)
    except (ImportError, ModuleNotFoundError) as e:
        raise ImportError(f"failed to import predictor module: {e}") from e

    fullname = f"{module_name}.{predictor_name}"
    if not hasattr(module, predictor_name):
        # Check if module is partially loaded (common with import errors)
        if module_name in sys.modules:
            raise ImportError(
                f"predictor {predictor_name} not found in {module_name} "
                "(module may have import errors)"
            )
        raise ValueError(f"predictor not found: {fullname}")

    p = getattr(module, predictor_name)

    if inspect.isclass(p):
        if not hasattr(p, "predict"):
            raise ValueError(f"predict method not found: {fullname}")

        if hasattr(p, "setup"):
            _validate_setup(_unwrap(getattr(p, "setup")))

        predict_fn_name = "predict"
        predict_fn = _unwrap(getattr(p, predict_fn_name))
        is_class_fn = True

    elif inspect.isfunction(p):
        predict_fn_name = predictor_name
        predict_fn = _unwrap(p)
        is_class_fn = False

    else:
        raise ValueError(f"invalid predictor {fullname}")

    # Find coders before validating predict function
    _find_coders(module)

    return _create_predictor_info(
        module_name, predictor_name, predict_fn, predict_fn_name, is_class_fn
    )


def check_input(
    inputs: Dict[str, adt.InputField], values: Dict[str, Any]
) -> Dict[str, Any]:
    """
    Validate and normalize input values against InputField definitions.

    Args:
        inputs: Dictionary of InputField definitions
        values: Dictionary of input values to validate

    Returns:
        Dictionary of normalized input values
    """
    kwargs: Dict[str, Any] = {}

    # Process provided values
    for name, value in values.items():
        input_field = inputs.get(name)
        if input_field is None:
            print(f"WARNING unknown input field ignored: {name}")
        else:
            try:
                kwargs[name] = input_field.type.normalize(value)
            except ValueError as e:
                # Reformat normalize errors to use "field: message" format
                # and avoid leaking user input values
                msg = str(e)
                if "failed to normalize value" in msg:
                    # Extract just the type name without the value
                    if " as " in msg:
                        type_name = msg.split(" as ", 1)[1]
                        raise ValueError(f"{name}: Invalid value for type {type_name}")
                    raise ValueError(f"{name}: Invalid value")
                # For other normalize errors, prepend field name
                raise ValueError(f"{name}: {msg}")

    # Apply defaults for missing values
    for name, input_field in inputs.items():
        if name not in kwargs:
            if isinstance(input_field.default, Field):
                if input_field.default.default_factory is not MISSING:
                    kwargs[name] = input_field.default.default_factory()
                elif input_field.default.default is not MISSING:
                    kwargs[name] = input_field.default.default
                else:
                    if input_field.type.repetition is not adt.Repetition.OPTIONAL:
                        raise ValueError(f"{name}: Field required")
                    kwargs[name] = None
            elif input_field.default is not None:
                kwargs[name] = input_field.default
            else:
                if input_field.type.repetition is not adt.Repetition.OPTIONAL:
                    raise ValueError(f"{name}: missing required input field")
                kwargs[name] = None

        # Validate constraints
        v = kwargs[name]
        values_to_check = []
        if input_field.type.repetition is adt.Repetition.REQUIRED:
            values_to_check = [v]
        elif input_field.type.repetition is adt.Repetition.OPTIONAL:
            values_to_check = [] if v is None else [v]
        elif input_field.type.repetition is adt.Repetition.REPEATED:
            values_to_check = v

        if input_field.ge is not None:
            if not all(x >= input_field.ge for x in values_to_check):
                raise ValueError(
                    f"{name} fails constraint >= {int(input_field.ge) if input_field.ge == int(input_field.ge) else input_field.ge}"
                )

        if input_field.le is not None:
            if not all(x <= input_field.le for x in values_to_check):
                raise ValueError(
                    f"{name} fails constraint <= {int(input_field.le) if input_field.le == int(input_field.le) else input_field.le}"
                )

        if input_field.min_length is not None:
            if not all(len(x) >= input_field.min_length for x in values_to_check):
                raise ValueError(
                    f"{name} fails constraint len() >= {input_field.min_length}"
                )

        if input_field.max_length is not None:
            if not all(len(x) <= input_field.max_length for x in values_to_check):
                raise ValueError(
                    f"{name} fails constraint len() <= {input_field.max_length}"
                )

        if input_field.regex is not None:
            p = re.compile(input_field.regex)
            if not all(p.match(x) is not None for x in values_to_check):
                raise ValueError(f"{name} does not match regex {input_field.regex!r}")

        if input_field.choices is not None:
            if not all(x in input_field.choices for x in values_to_check):
                raise ValueError(
                    f"{name} does not match choices {input_field.choices!r}"
                )

    return kwargs
