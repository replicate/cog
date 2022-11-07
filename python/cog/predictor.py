from abc import ABC, abstractmethod
from collections.abc import Iterator
import enum
import importlib.util
import inspect
import os.path
from pathlib import Path
from pydantic import create_model, BaseModel, Field
from pydantic.fields import FieldInfo
from typing import Any, Callable, Dict, List, Type

# Added in Python 3.8. Can be from typing if we drop support for <3.8.
from typing_extensions import get_origin, get_args, Annotated
import yaml

from .errors import ConfigDoesNotExist, PredictorNotSet
from .types import Input, Path as CogPath, File as CogFile


ALLOWED_INPUT_TYPES = [str, int, float, bool, CogFile, CogPath]


class BasePredictor(ABC):
    def setup(self) -> None:
        """
        An optional method to prepare the model so multiple predictions run efficiently.
        """

    @abstractmethod
    def predict(self, **kwargs: Any) -> Any:
        """
        Run a single prediction on the model
        """


def run_prediction(
    predictor: BasePredictor, inputs: Dict[Any, Any], cleanup_functions: List[Callable]
) -> Any:
    """
    Run the predictor on the inputs, and append resulting paths
    to cleanup functions for removal.
    """
    result = predictor.predict(**inputs)
    if isinstance(result, Path):
        cleanup_functions.append(result.unlink)
    return result


# TODO: make config a TypedDict
def load_config() -> Dict[str, Any]:
    """
    Reads cog.yaml and returns it as a dict.
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
    return config


def load_predictor(config: Dict[str, Any]) -> BasePredictor:
    """
    Constructs an instance of the user-defined Predictor class from a config.
    """

    ref = get_predictor_ref(config)
    return load_predictor_from_ref(ref)


def get_predictor_ref(config: Dict[str, Any]) -> str:
    if "predict" not in config:
        raise PredictorNotSet(
            "Can't run predictions: 'predict' option not found in cog.yaml"
        )

    return config["predict"]


def load_predictor_from_ref(ref: str) -> BasePredictor:
    module_path, class_name = ref.split(":", 1)
    module_name = os.path.basename(module_path).split(".py", 1)[0]
    spec = importlib.util.spec_from_file_location(module_name, module_path)
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    predictor_class = getattr(module, class_name)
    return predictor_class()


# Base class for inputs, constructed dynamically in get_input_type().
# (This can't be a docstring or it gets passed through to the schema.)
class BaseInput(BaseModel):
    class Config:
        # When using `choices`, the type is converted into an enum to validate
        # But, after validation, we want to pass the actual value to predict(), not the enum object
        use_enum_values = True

    def cleanup(self) -> None:
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


def get_input_type(predictor: BasePredictor) -> Type[BaseInput]:
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

                InputType = StringEnum(  # type: ignore
                    name, {value: value for value in choices}
                )
            elif InputType == int:
                InputType = enum.IntEnum(name, {str(value): value for value in choices})  # type: ignore
            else:
                raise TypeError(
                    f"The input {name} uses the option choices. Choices can only be used with str or int types."
                )

        create_model_kwargs[name] = (InputType, default)

    return create_model(
        "Input",
        __config__=None,
        __base__=BaseInput,
        __module__=__name__,
        __validators__=None,
        **create_model_kwargs,
    )


def get_output_type(predictor: BasePredictor) -> Type[BaseModel]:
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
        # Annotated allows us to attach Field annotations to the list, which we use to mark that this is an iterator
        # https://pydantic-docs.helpmanual.io/usage/schema/#typingannotated-fields
        OutputType = Annotated[List[get_args(OutputType)[0]], Field(**{"x-cog-array-type": "iterator"})]  # type: ignore

    if not hasattr(OutputType, "__name__") or OutputType.__name__ != "Output":
        # Wrap the type in a model called "Output" so it is a consistent name in the OpenAPI schema
        class Output(BaseModel):
            __root__: OutputType  # type: ignore

        OutputType = Output

    return OutputType


def human_readable_type_name(t: Type) -> str:
    """
    Generates a useful-for-humans label for a type. For builtin types, it's just the class name (eg "str" or "int"). For other types, it includes the module (eg "pathlib.Path" or "cog.File").

    The special case for Cog modules is because the type lives in `cog.types` internally, but just `cog` when included as a dependency.
    """
    module = t.__module__
    if module == "builtins":
        return t.__qualname__
    elif module.split(".")[0] == "cog":
        module = "cog"

    try:
        return module + "." + t.__qualname__
    except AttributeError:
        return str(t)


def readable_types_list(type_list: List[Type]) -> str:
    return ", ".join(human_readable_type_name(t) for t in type_list)
