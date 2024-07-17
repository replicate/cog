import enum
import importlib.util
import inspect
import io
import os.path
import sys
import types
import uuid
from abc import ABC, abstractmethod
from collections.abc import Iterator
from pathlib import Path
from typing import (
    Any,
    Callable,
    Dict,
    List,
    Optional,
    Type,
    Union,
    cast,
    get_type_hints,
)
from unittest.mock import patch

import structlog

import cog.code_xforms as code_xforms

try:
    from typing import get_args, get_origin
except ImportError:  # Python < 3.8
    from typing_compat import get_args, get_origin  # type: ignore

import yaml
from pydantic import BaseModel, Field, create_model
from pydantic.fields import FieldInfo

# Added in Python 3.9. Can be from typing if we drop support for <3.9
from typing_extensions import Annotated

from .errors import ConfigDoesNotExist, PredictorNotSet
from .types import (
    CogConfig,
    Input,
    URLPath,
)
from .types import (
    File as CogFile,
)
from .types import (
    Path as CogPath,
)
from .types import Secret as CogSecret

log = structlog.get_logger("cog.server.predictor")

ALLOWED_INPUT_TYPES: List[Type[Any]] = [
    str,
    int,
    float,
    bool,
    CogFile,
    CogPath,
    CogSecret,
]


class BasePredictor(ABC):
    def setup(self, weights: Optional[Union[CogFile, CogPath, str]] = None) -> None:
        """
        An optional method to prepare the model so multiple predictions run efficiently.
        """
        return

    @abstractmethod
    def predict(self, **kwargs: Any) -> Any:
        """
        Run a single prediction on the model
        """
        pass


def run_setup(predictor: BasePredictor) -> None:
    weights_type = get_weights_type(predictor.setup)

    # No weights need to be passed, so just run setup() without any arguments.
    if weights_type is None:
        predictor.setup()
        return

    weights: Union[io.IOBase, Path, str, None]

    weights_url = os.environ.get("COG_WEIGHTS")
    weights_path = "weights"

    # TODO: Cog{File,Path}.validate(...) methods accept either "real"
    # paths/files or URLs to those things. In future we can probably tidy this
    # up a little bit.
    # TODO: CogFile/CogPath should have subclasses for each of the subtypes
    if weights_url:
        if weights_type == CogFile:
            weights = cast(CogFile, CogFile.validate(weights_url))
        elif weights_type == CogPath:
            # TODO: So this can be a url. evil!
            weights = cast(CogPath, CogPath.validate(weights_url))
        # allow people to download weights themselves
        elif weights_type == str:  # noqa: E721
            weights = weights_url
        else:
            raise ValueError(
                f"Predictor.setup() has an argument 'weights' of type {weights_type}, but only File, Path and str are supported"
            )
    elif os.path.exists(weights_path):
        if weights_type == CogFile:
            weights = cast(CogFile, open(weights_path, "rb"))
        elif weights_type == CogPath:
            weights = CogPath(weights_path)
        else:
            raise ValueError(
                f"Predictor.setup() has an argument 'weights' of type {weights_type}, but only File, Path and str are supported"
            )
    else:
        weights = None

    predictor.setup(weights=weights)


def get_weights_type(setup_function: Callable[[Any], None]) -> Optional[Any]:
    signature = inspect.signature(setup_function)
    if "weights" not in signature.parameters:
        return None
    Type = signature.parameters["weights"].annotation
    # Handle Optional. It is Union[Type, None]
    if get_origin(Type) == Union:
        args = get_args(Type)
        if len(args) == 2 and args[1] is type(None):
            Type = get_args(Type)[0]
    return Type


def run_prediction(
    predictor: BasePredictor,
    inputs: Dict[Any, Any],
    cleanup_functions: List[Callable[[], None]],
) -> Any:
    """
    Run the predictor on the inputs, and append resulting paths
    to cleanup functions for removal.
    """
    result = predictor.predict(**inputs)
    if isinstance(result, Path):
        cleanup_functions.append(result.unlink)
    return result


def load_config() -> CogConfig:
    """
    Reads cog.yaml and returns it as a typed dict.
    """
    # Assumes the working directory is /src
    config_path = os.path.abspath("cog.yaml")
    try:
        with open(config_path) as fh:
            config = yaml.safe_load(fh)
    except FileNotFoundError as e:
        raise ConfigDoesNotExist(
            f"Could not find {config_path}",
        ) from e
    return config


def load_predictor(config: CogConfig) -> BasePredictor:
    """
    Constructs an instance of the user-defined Predictor class from a config.
    """

    ref = get_predictor_ref(config)
    return load_predictor_from_ref(ref)


def get_predictor_ref(config: CogConfig, mode: str = "predict") -> str:
    if mode not in ["predict", "train"]:
        raise ValueError(f"Invalid mode: {mode}")

    if mode not in config:
        raise PredictorNotSet(
            f"Can't run predictions: '{mode}' option not found in cog.yaml"
        )

    return config[mode]


def load_full_predictor_from_file(
    module_path: str, module_name: str
) -> types.ModuleType:
    spec = importlib.util.spec_from_file_location(module_name, module_path)
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    # Remove any sys.argv while importing predictor to avoid conflicts when
    # user code calls argparse.Parser.parse_args in production
    with patch("sys.argv", sys.argv[:1]):
        spec.loader.exec_module(module)
    return module


def load_slim_predictor_from_file(
    module_path: str, class_name: str, method_name: str
) -> Optional[types.ModuleType]:
    with open(module_path, encoding="utf-8") as file:
        source_code = file.read()
    stripped_source = code_xforms.strip_model_source_code(
        source_code, class_name, method_name
    )
    module = code_xforms.load_module_from_string(uuid.uuid4().hex, stripped_source)
    return module


def get_predictor(module: types.ModuleType, class_name: str) -> Any:
    predictor = getattr(module, class_name)
    # It could be a class or a function
    if inspect.isclass(predictor):
        return predictor()
    return predictor


def load_slim_predictor_from_ref(ref: str, method_name: str) -> BasePredictor:
    module_path, class_name = ref.split(":", 1)
    module_name = os.path.basename(module_path).split(".py", 1)[0]
    module = None
    try:
        if sys.version_info >= (3, 9):
            module = load_slim_predictor_from_file(module_path, class_name, method_name)
            if not module:
                log.debug(f"[{module_name}] fast loader returned None")
        else:
            log.debug(f"[{module_name}] cannot use fast loader as current Python <3.9")
    except Exception as e:
        log.debug(f"[{module_name}] fast loader failed: {e}")
    finally:
        if not module:
            log.debug(f"[{module_name}] falling back to slow loader")
            module = load_full_predictor_from_file(module_path, module_name)
    predictor = get_predictor(module, class_name)
    return predictor


def load_predictor_from_ref(ref: str) -> BasePredictor:
    module_path, class_name = ref.split(":", 1)
    module_name = os.path.basename(module_path).split(".py", 1)[0]
    module = load_full_predictor_from_file(module_path, module_name)
    predictor = get_predictor(module, class_name)
    return predictor


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
            # Handle URLPath objects specially for cleanup.
            # Also handle pathlib.Path objects, which cog.Path is a subclass of.
            # A pathlib.Path object shouldn't make its way here,
            # but both have an unlink() method, so we may as well be safe.
            if isinstance(value, (URLPath, Path)):
                value.unlink(missing_ok=True)


def validate_input_type(type: Type[Any], name: str) -> None:
    if type is inspect.Signature.empty:
        raise TypeError(
            f"No input type provided for parameter `{name}`. Supported input types are: {readable_types_list(ALLOWED_INPUT_TYPES)}, or a Union or List of those types."
        )
    elif type not in ALLOWED_INPUT_TYPES:
        if get_origin(type) in (Union, List, list) or (
            hasattr(types, "UnionType") and get_origin(type) is types.UnionType
        ):  # noqa: E721
            for t in get_args(type):
                validate_input_type(t, name)
        else:
            raise TypeError(
                f"Unsupported input type {human_readable_type_name(type)} for parameter `{name}`. Supported input types are: {readable_types_list(ALLOWED_INPUT_TYPES)}, or a Union or List of those types."
            )


def get_input_create_model_kwargs(
    signature: inspect.Signature, input_types: Dict[str, Any]
) -> Dict[str, Any]:
    create_model_kwargs = {}

    order = 0

    for name, parameter in signature.parameters.items():
        if name not in input_types:
            raise TypeError(f"No input type provided for parameter `{name}`.")

        InputType = input_types[name]

        validate_input_type(InputType, name)

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
            if InputType == str:  # noqa: E721

                class StringEnum(str, enum.Enum):
                    pass

                InputType = StringEnum(  # type: ignore
                    name, {value: value for value in choices}
                )
            elif InputType == int:  # noqa: E721
                InputType = enum.IntEnum(name, {str(value): value for value in choices})  # type: ignore
            else:
                raise TypeError(
                    f"The input {name} uses the option choices. Choices can only be used with str or int types."
                )

        create_model_kwargs[name] = (InputType, default)

    return create_model_kwargs


def get_predict(predictor: Any) -> Callable[..., Any]:
    if hasattr(predictor, "predict"):
        return predictor.predict
    return predictor


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

    predict = get_predict(predictor)
    signature = inspect.signature(predict)

    input_types = get_type_hints(predict)
    if "return" in input_types:
        del input_types["return"]

    return create_model(
        "Input",
        __config__=None,
        __base__=BaseInput,
        __module__=__name__,
        __validators__=None,
        **get_input_create_model_kwargs(signature, input_types),
    )  # type: ignore


def get_output_type(predictor: BasePredictor) -> Type[BaseModel]:
    """
    Creates a Pydantic Output model from the return type annotation of a Predictor's predict() method.
    """

    predict = get_predict(predictor)

    input_types = get_type_hints(predict)

    OutputType = input_types.pop("return", None)
    if OutputType is None:
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

    # The type that goes in the response is a list of the yielded type
    if get_origin(OutputType) is Iterator:
        # Annotated allows us to attach Field annotations to the list, which we use to mark that this is an iterator
        # https://pydantic-docs.helpmanual.io/usage/schema/#typingannotated-fields
        field = Field(**{"x-cog-array-type": "iterator"})  # type: ignore
        OutputType: Type[BaseModel] = Annotated[List[get_args(OutputType)[0]], field]  # type: ignore

    name = OutputType.__name__ if hasattr(OutputType, "__name__") else ""

    if name == "Output":
        return OutputType

    # We wrap the OutputType in an Output class to
    # ensure consistent naming of the interface in the schema.
    #
    # NOTE: If the OutputType.__name__ is "TrainingOutput" then cannot use
    # `__root__` here because this will create a reference for the Object.
    # e.g.
    #   {'title': 'Output', '$ref': '#/definitions/TrainingOutput' ... }
    #
    # And this reference may conflict with other objects at which
    # point the item will be namespaced and break our parsing. e.g.
    #   {'title': 'Output', '$ref': '#/definitions/predict_TrainingOutput' ... }
    #
    # So we work around this by inheriting from the original class rather
    # than using "__root__".
    if name == "TrainingOutput":

        class Output(OutputType):  # type: ignore
            pass

        return Output
    else:

        class Output(BaseModel):
            __root__: OutputType  # type: ignore

        return Output


def get_train(predictor: Any) -> Callable[..., Any]:
    if hasattr(predictor, "train"):
        return predictor.train
    return predictor


def get_training_input_type(predictor: BasePredictor) -> Type[BaseInput]:
    """
    Creates a Pydantic Input model from the arguments of a Predictor's train() method.

    def train(self, text: str):
        ...

    programmatically creates a model like this:

    class TrainingInput(BaseModel):
        text: str
    """

    train = get_train(predictor)
    signature = inspect.signature(train)

    input_types = get_type_hints(train)
    if "return" in input_types:
        del input_types["return"]

    return create_model(
        "TrainingInput",
        __config__=None,
        __base__=BaseInput,
        __module__=__name__,
        __validators__=None,
        **get_input_create_model_kwargs(signature, input_types),
    )  # type: ignore


def get_training_output_type(predictor: BasePredictor) -> Type[BaseModel]:
    """
    Creates a Pydantic Output model from the return type annotation of a train() method.
    """

    train = get_train(predictor)

    input_types = get_type_hints(train)
    TrainingOutputType = input_types.pop("return", None)
    if TrainingOutputType is None:
        raise TypeError(
            """You must set an output type. If your model can return multiple output types, you can explicitly set `Any` as the output type.

For example:

    from typing import Any

    def train(
        self,
        n: int
    ) -> Any:
        ...
"""
        )

    name = (
        TrainingOutputType.__name__ if hasattr(TrainingOutputType, "__name__") else ""
    )
    # We wrap the OutputType in a TrainingOutput class to
    # ensure consistent naming of the interface in the schema
    # See comment in get_output_type for more info.
    if name == "TrainingOutput":
        return TrainingOutputType

    if name == "Output":

        class TrainingOutput(TrainingOutputType):  # type: ignore
            pass

        return TrainingOutput

    class TrainingOutput(BaseModel):
        __root__: TrainingOutputType  # type: ignore

    return TrainingOutput


def human_readable_type_name(t: Type[Union[Any, None]]) -> str:
    """
    Generates a useful-for-humans label for a type. For builtin types, it's just the class name (eg "str" or "int"). For other types, it includes the module (eg "pathlib.Path" or "cog.File").

    The special case for Cog modules is because the type lives in `cog.types` internally, but just `cog` when included as a dependency.
    """

    if hasattr(t, "__module__"):
        module = t.__module__
        if module == "builtins":
            return t.__qualname__
        elif module.split(".")[0] == "cog":
            module = "cog"

        try:
            return f"{module}.{t.__qualname__}"
        except AttributeError:
            pass

    return str(t)


def readable_types_list(type_list: List[Type[Any]]) -> str:
    return ", ".join(human_readable_type_name(t) for t in type_list)
