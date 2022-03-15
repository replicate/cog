from abc import ABC, abstractmethod
from collections.abc import Iterator
import enum
import importlib
import inspect
import os.path
from pathlib import Path
from pydantic import create_model, BaseModel
from pydantic.fields import FieldInfo
from typing import List

# Added in Python 3.8. Can be from typing if we drop support for <3.8.
from typing_extensions import get_origin, get_args
import yaml

from .errors import ConfigDoesNotExist, PredictorNotSet
from .types import Input, Path as CogPath, File as CogFile


ALLOWED_INPUT_TYPES = [str, int, float, bool, CogFile, CogPath]


class BasePredictor(ABC):
    def setup(self):
        """
        An optional method to prepare the model so multiple predictions run efficiently.
        """

    @abstractmethod
    def predict(self, **kwargs):
        """
        Run a single prediction on the model.
        """


def run_prediction(predictor, inputs, cleanup_functions):
    """
    Run the predictor on the inputs, and append resulting paths
    to cleanup functions for removal.
    """
    result = predictor.predict(**inputs)
    if isinstance(result, Path):
        cleanup_functions.append(result.unlink)
    return result


def load_predictor():
    """
    Reads cog.yaml and constructs an instance of the user-defined Predictor class.
    """

    # Assumes the working directory is /src
    config_path = os.path.abspath("cog.yaml")
    try:
        with open(config_path) as fh:
            config = yaml.safe_load(fh)
    except FileNotFoundError:
        raise ConfigDoesNotExist(
            f"Could not find {config_path}",
        )

    if "predict" not in config:
        raise PredictorNotSet(
            "Can't run predictions: 'predict' option not found in cog.yaml"
        )

    predict_string = config["predict"]
    module_path, class_name = predict_string.split(":", 1)
    module_name = os.path.basename(module_path).split(".py", 1)[0]
    spec = importlib.util.spec_from_file_location(module_name, module_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    predictor_class = getattr(module, class_name)
    return predictor_class()


# Base class for inputs, constructed dynamically in get_input_type().
# (This can't be a docstring or it gets passed through to the schema.)
class BaseInput(BaseModel):
    def cleanup(self):
        """
        Cleanup any temporary files created by the input.
        """
        for _, value in self:
            # Note this is pathlib.Path, which cog.Path is a subclass of. A pathlib.Path object shouldn't make its way here,
            # but both have an unlink() method, so may as well be safe.
            if isinstance(value, Path):
                # This could be missing_ok=True when we drop support for Python 3.7
                if value.exists():
                    value.unlink()


def get_input_type(predictor: BasePredictor):
    """
    Creates a Pydantic Input model from the arguments of a Predictor's predict() method.

    class Predictor(BasePredictor):
        def predict(self, text: str):
            ...

    programmatically creates a model like this:

    class Input(BaseModel):
        text: str
    """

    signature = inspect.signature(predictor.predict)
    create_model_kwargs = {}

    order = 0

    for name, parameter in signature.parameters.items():
        InputType = parameter.annotation

        if InputType is inspect.Signature.empty:
            raise TypeError(
                f"No input type provided for parameter `{name}`. Supported input types are: {readable_types_list(ALLOWED_INPUT_TYPES)}."
            )
        elif InputType not in ALLOWED_INPUT_TYPES:
            raise TypeError(
                f"Unsupported input type {human_readable_type_name(InputType)} for parameter `{name}`. Supported input types are: {readable_types_list(ALLOWED_INPUT_TYPES)}."
            )

        # if no default is specified, create an empty, required input
        if parameter.default is inspect.Signature.empty:
            default = Input()
        else:
            default = parameter.default
            # If user hasn't used `Input`, then wrap it in that
            if not isinstance(default, FieldInfo):
                default = Input(default=default)

        # Fields aren't ordered, so use this pattern to ensure defined order
        # https://github.com/go-openapi/spec/pull/116
        default.extra["x-order"] = order
        order += 1

        # Choices!
        if default.extra.get("choices"):
            choices = default.extra["choices"]
            # It will be passed automatically as 'enum' in the schema, so remove it as an extra field.
            del default.extra["choices"]
            if InputType == str:
                class StringEnum(str, enum.Enum):
                    pass
                InputType = StringEnum(name, {value: value for value in choices})
            elif InputType == int:
                InputType = enum.IntEnum(name, {str(value): value for value in choices})
            else:
                raise TypeError(
                    f"The input {name} uses the option choices. Choices can only be used with str or int types."
                )


        create_model_kwargs[name] = (InputType, default)

    return create_model("Input", **create_model_kwargs, __base__=BaseInput)


def get_output_type(predictor: BasePredictor):
    """
    Creates a Pydantic Output model from the return type annotation of a Predictor's predict() method.
    """

    signature = inspect.signature(predictor.predict)
    if signature.return_annotation is inspect.Signature.empty:
        raise TypeError(
            """You must set an output type. If your model can return multiple output types, you can explicitly set `Any` as the output type.

For example:

    from typing import Any

    def predict(
        self,
        image: Path = Input(description="Input image"),
    ) -> Any:
        ...
"""
        )
    else:
        OutputType = signature.return_annotation

    # The type that goes in the response is a list of the yielded type
    if get_origin(OutputType) is Iterator:
        OutputType = List[get_args(OutputType)[0]]

    if not hasattr(OutputType, "__name__") or OutputType.__name__ != "Output":
        # Wrap the type in a model called "Output" so it is a consistent name in the OpenAPI schema
        class Output(BaseModel):
            __root__: OutputType

        OutputType = Output

    return OutputType


def human_readable_type_name(t):
    """
    Generates a useful-for-humans label for a type. For builtin types, it's just the class name (eg "str" or "int"). For other types, it includes the module (eg "pathlib.Path" or "cog.File").

    The special case for Cog modules is because the type lives in `cog.types` internally, but just `cog` when included as a dependency.
    """
    module = t.__module__
    if module == "builtins":
        return t.__qualname__
    elif module.split(".")[0] == "cog":
        module = "cog"
    return module + "." + t.__qualname__


def readable_types_list(type_list):
    return ", ".join(human_readable_type_name(t) for t in type_list)
