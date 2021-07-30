from dataclasses import dataclass
import functools
from numbers import Number
import os
from pathlib import Path
import shutil
import tempfile
from typing import Any, Optional, List, Callable, Dict, Type

from werkzeug.datastructures import FileStorage

from .predictor import Predictor

_VALID_INPUT_TYPES = frozenset([str, int, float, bool, Path])
UNSPECIFIED = object()


@dataclass
class InputSpec:
    type: Type
    default: Any = UNSPECIFIED
    min: Optional[Number] = None
    max: Optional[Number] = None
    options: Optional[List[Any]] = None
    help: Optional[str] = None


class InputValidationError(Exception):
    pass


def input(name, type, default=UNSPECIFIED, min=None, max=None, options=None, help=None):
    """
    A decorator that defines an input for a predict() method.
    """
    type_name = get_type_name(type)
    if type not in _VALID_INPUT_TYPES:
        type_list = ", ".join([type_name(t) for t in _VALID_INPUT_TYPES])
        raise ValueError(
            f"{type_name} is not a valid input type. Valid types are: {type_list}"
        )
    if (min is not None or max is not None) and not _is_numeric_type(type):
        raise ValueError(f"Non-numeric type {type_name} cannot have min and max values")

    if options is not None and type == Path:
        raise ValueError(f"File type cannot have options")

    if options is not None and len(options) < 2:
        raise ValueError(f"Options list must have at least two items")

    def wrapper(f):
        if not hasattr(f, "_inputs"):
            f._inputs = {}

        if name in f._inputs:
            raise ValueError(f"{name} is already defined as an argument")

        if type == Path and default is not UNSPECIFIED and default is not None:
            raise TypeError("Cannot use default with Path type")

        f._inputs[name] = InputSpec(
            type=type, default=default, min=min, max=max, options=options, help=help
        )

        @functools.wraps(f)
        def wraps(self, **kwargs):
            if not isinstance(self, Predictor):
                raise TypeError("{self} is not an instance of cog.Predictor")
            return f(self, **kwargs)

        return wraps

    return wrapper


def get_type_name(typ: Type) -> str:
    if typ == str:
        return "str"
    if typ == int:
        return "int"
    if typ == float:
        return "float"
    if typ == bool:
        return "bool"
    if typ == Path:
        return "Path"
    return str(typ)


def _is_numeric_type(typ: Type) -> bool:
    return typ in (int, float)


def validate_and_convert_inputs(
    predictor: Predictor, raw_inputs: Dict[str, Any], cleanup_functions: List[Callable]
) -> Dict[str, Any]:
    input_specs = predictor.predict._inputs
    inputs = {}

    for name, input_spec in input_specs.items():
        if name in raw_inputs:
            val = raw_inputs[name]

            if input_spec.type == Path:
                if not isinstance(val, FileStorage):
                    raise InputValidationError(
                        f"Could not convert file input {name} to {get_type_name(input_spec.type)}",
                    )
                if val.filename is None:
                    raise InputValidationError(
                        f"No filename is provided for file input {name}"
                    )

                temp_dir = tempfile.mkdtemp()
                cleanup_functions.append(lambda: shutil.rmtree(temp_dir))

                temp_path = os.path.join(temp_dir, val.filename)
                with open(temp_path, "wb") as f:
                    f.write(val.stream.read())
                converted = Path(temp_path)

            elif input_spec.type == int:
                try:
                    converted = int(val)
                except ValueError:
                    raise InputValidationError(f"Could not convert {name}={val} to int")

            elif input_spec.type == float:
                try:
                    converted = float(val)
                except ValueError:
                    raise InputValidationError(
                        f"Could not convert {name}={val} to float"
                    )

            elif input_spec.type == bool:
                if val.lower() not in ["true", "false"]:
                    raise InputValidationError(f"{name}={val} is not a boolean")
                converted = val.lower() == "true"

            elif input_spec.type == str:
                if isinstance(val, FileStorage):
                    raise InputValidationError(
                        f"Could not convert file input {name} to str"
                    )
                converted = val

            else:
                raise TypeError(
                    f"Internal error: Input type {input_spec} is not a valid input type"
                )

            if _is_numeric_type(input_spec.type):
                if input_spec.max is not None and converted > input_spec.max:
                    raise InputValidationError(
                        f"Value {converted} is greater than the max value {input_spec.max}"
                    )
                if input_spec.min is not None and converted < input_spec.min:
                    raise InputValidationError(
                        f"Value {converted} is less than the min value {input_spec.min}"
                    )

            if input_spec.options is not None:
                if converted not in input_spec.options:
                    valid_str = ", ".join([str(o) for o in input_spec.options])
                    raise InputValidationError(
                        f"Value {converted} is not an option. Valid options are: {valid_str}"
                    )

        else:
            if input_spec.default is not UNSPECIFIED:
                converted = input_spec.default
            else:
                raise InputValidationError(f"Missing expected argument: {name}")
        inputs[name] = converted

    expected_keys = set(input_specs.keys())
    raw_keys = set(raw_inputs.keys())
    extraneous_keys = raw_keys - expected_keys
    if extraneous_keys:
        raise InputValidationError(
            f"Extraneous input keys: {', '.join(extraneous_keys)}"
        )

    return inputs
