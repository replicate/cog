import builtins
import enum
import importlib.util
import inspect
import os.path
import sys
import types
import uuid
from collections.abc import Iterable, Iterator

if sys.version_info >= (3, 10):
    from types import NoneType
from typing import (
    Any,
    Callable,
    Dict,
    List,
    Literal,
    Optional,
    Type,
    Union,
    cast,
    get_args,
    get_origin,
)
from unittest.mock import patch

import pydantic
import structlog
from pydantic import BaseModel, Field, create_model
from pydantic.fields import FieldInfo

# Added in Python 3.9. Can be from typing if we drop support for <3.9
from typing_extensions import Annotated

from .base_input import BaseInput
from .base_predictor import BasePredictor
from .code_xforms import load_module_from_string, strip_model_source_code
from .types import (
    PYDANTIC_V2,
    Input,
    Weights,
)
from .types import (
    File as CogFile,
)
from .types import (
    Path as CogPath,
)
from .types import Secret as CogSecret

if PYDANTIC_V2:
    from pydantic.fields import PydanticUndefined  # type: ignore
else:
    from pydantic.fields import Undefined as PydanticUndefined

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


def has_setup_weights(predictor: BasePredictor) -> bool:
    weights_type = get_weights_type(predictor.setup)
    return weights_type is not None


def extract_setup_weights(predictor: BasePredictor) -> Optional[Weights]:
    weights_type = get_weights_type(predictor.setup)
    assert weights_type

    weights: Optional[Weights]

    weights_url = os.environ.get("COG_WEIGHTS")
    weights_path = "weights"

    # TODO: Cog{File,Path}.validate(...) methods accept either "real"
    # paths/files or URLs to those things. In future we can probably tidy this
    # up a little bit.
    # TODO: CogFile/CogPath should have subclasses for each of the subtypes
    if weights_url:
        if PYDANTIC_V2:
            from pydantic import TypeAdapter

            for t in [CogFile, CogPath]:
                try:
                    weights = TypeAdapter(t).validate_python(weights_url)
                    break
                except Exception:  # pylint: disable=broad-except # noqa: S110
                    pass
            else:
                if weights_type is str:
                    weights = weights_url
                else:
                    raise ValueError(
                        f"Predictor.setup() has an argument 'weights' of type {weights_type}, but only File, Path and str are supported"
                    )
        else:
            if weights_type is CogFile:
                weights = cast(CogFile, CogFile.validate(weights_url))
            elif weights_type is CogPath:
                # TODO: So this can be a url. evil!
                weights = cast(CogPath, CogPath.validate(weights_url))
            elif weights_type is str:
                weights = weights_url
            else:
                raise ValueError(
                    f"Predictor.setup() has an argument 'weights' of type {weights_type}, but only File, Path and str are supported"
                )
    elif os.path.exists(weights_path):
        if weights_type == CogFile:
            with open(weights_path, "rb") as f:
                weights = cast(CogFile, f)
        elif weights_type == CogPath:
            weights = CogPath(weights_path)
        else:
            raise ValueError(
                f"Predictor.setup() has an argument 'weights' of type {weights_type}, but only File, Path and str are supported"
            )
    else:
        weights = None

    return weights


def get_weights_type(setup_function: Callable[[Any], None]) -> Optional[Any]:
    signature = inspect.signature(setup_function)
    if "weights" not in signature.parameters:
        return None
    Type = signature.parameters["weights"].annotation  # pylint: disable=invalid-name,redefined-outer-name
    # Handle Optional. It is Union[Type, None]
    if get_origin(Type) == Union:
        args = get_args(Type)
        if len(args) == 2 and args[1] is type(None):
            Type = get_args(Type)[0]  # pylint: disable=invalid-name
    return Type


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
    stripped_source = strip_model_source_code(source_code, [class_name], [method_name])
    module = load_module_from_string(uuid.uuid4().hex, stripped_source)
    return module


def get_predictor(module: types.ModuleType, class_name: str) -> Any:
    predictor = getattr(module, class_name)
    # It could be a class or a function
    if inspect.isclass(predictor):
        return predictor()
    return predictor


def load_predictor_from_ref(ref: str) -> BasePredictor:
    module_path, class_name = ref.split(":", 1)
    module_name = os.path.basename(module_path).split(".py", 1)[0]
    module = load_full_predictor_from_file(module_path, module_name)
    predictor = get_predictor(module, class_name)
    return predictor


def is_none(type_arg: Any) -> bool:
    if sys.version_info >= (3, 10):
        return type_arg is NoneType
    return type_arg is None.__class__


def is_union(type: Type[Any]) -> bool:
    if get_origin(type) is Union:
        return True
    if hasattr(types, "UnionType") and get_origin(type) is types.UnionType:
        return True
    return False


def is_optional(type: Type[Any]) -> bool:
    args = get_args(type)
    if len(args) != 2 or not is_union(type):
        return False
    return is_none(args[1])


def validate_input_type(
    type: Type[Any],  # pylint: disable=redefined-builtin
    name: str,
) -> None:
    if type is inspect.Signature.empty:
        raise TypeError(
            f"No input type provided for parameter `{name}`. Supported input types are: {readable_types_list(ALLOWED_INPUT_TYPES)}, or a Union or List of those types."
        )
    if type not in ALLOWED_INPUT_TYPES:
        if get_origin(type) is Literal:
            for t in get_args(type):
                validate_input_type(builtins.type(t), name)
        elif get_origin(type) in (Union, List, list) or is_union(type):  # noqa: E721
            args = get_args(type)
            if is_optional(type):
                validate_input_type(args[0], name)
            else:
                for t in args:
                    validate_input_type(t, name)
        else:
            if PYDANTIC_V2:
                # Cog types are exported as `Annotated[Type, ...]`, but `type` is the inner type
                if hasattr(type, "__module__") and type.__module__ == "cog.types":
                    return

            raise TypeError(
                f"Unsupported input type {human_readable_type_name(type)} for parameter `{name}`. Supported input types are: {readable_types_list(ALLOWED_INPUT_TYPES)}, or a Union or List of those types."
            )


def get_input_create_model_kwargs(signature: inspect.Signature) -> Dict[str, Any]:
    create_model_kwargs: Dict[str, Any] = {
        "__base__": BaseInput,
        "__config__": None,
    }

    order = 0

    for name, parameter in signature.parameters.items():
        InputType = parameter.annotation

        if parameter.kind == inspect.Parameter.VAR_POSITIONAL:
            raise TypeError(f"Unsupported variadic positional parameter *{name}.")

        if parameter.kind == inspect.Parameter.VAR_KEYWORD:
            if order != 0:
                raise TypeError(f"Unsupported variadic keyword parameter **{name}")

            class ExtraKeywordInput(BaseInput):
                if PYDANTIC_V2:
                    model_config = pydantic.ConfigDict(extra="allow")
                else:

                    class Config:
                        extra = "allow"

            create_model_kwargs["__base__"] = ExtraKeywordInput
            name = "__pydantic_extra__"
            InputType = Dict[str, Any]

            create_model_kwargs[name] = (InputType, Input())
            continue

        validate_input_type(InputType, name)

        # if no default is specified, create an empty, required input
        if parameter.default is inspect.Signature.empty:
            default = Input()
        else:
            if not isinstance(parameter.default, FieldInfo):
                default = Input(default=parameter.default)
            else:
                if is_optional(InputType):
                    # If we are an optional, make sure the default is None
                    if (
                        parameter.default.default is PydanticUndefined
                        or parameter.default.default is ...
                    ):
                        parameter.default.default = None
                        if not PYDANTIC_V2:
                            parameter.default.default_factory = None
                default = parameter.default

        extra: Dict[str, Any] = {}
        if PYDANTIC_V2:
            # https://github.com/pydantic/pydantic/blob/2.7/pydantic/json_schema.py#L1436-L1446
            # json_schema_extra can be a callable, but we don't set that and users shouldn't set that
            if not default.json_schema_extra:  # type: ignore
                default.json_schema_extra = {"x-order": order}  # type: ignore
            assert isinstance(default.json_schema_extra, dict)  # type: ignore
            # In Pydantic 2.12.0 the json_schema_extra field is copied into a variable called "_attributes_set"
            # that gets created in the constructor.
            # This means that changes to that dictionary after the construction don't take effect during the render
            # to openapi schema JSON.
            # To get around this, we will reference the dictionary in the attributes_set variable and make changes to
            # json_schema_extra take effect.
            if hasattr(default, "_attributes_set"):
                if "json_schema_extra" not in default._attributes_set:  # type: ignore
                    default._attributes_set["json_schema_extra"] = {"x-order": order}
                extra = default._attributes_set["json_schema_extra"]  # type: ignore
            else:
                extra = default.json_schema_extra  # type: ignore
        else:
            extra = default.extra  # type: ignore
        extra["x-order"] = order
        order += 1

        # Choices!
        choices = (
            extra.pop("choices", None)  # Pydantic v1
            or extra.pop("enum", None)  # Pydantic v2
        )
        # In either case, remove it as an extra field because it will be
        # passed automatically as 'enum' in the schema
        if choices:
            if InputType == str and isinstance(choices, Iterable):  # noqa: E721

                class StringEnum(str, enum.Enum):
                    pass

                InputType = StringEnum(  # pylint: disable=invalid-name
                    name, [(value, value) for value in choices or []]
                )
            elif InputType == int:  # noqa: E721
                InputType = enum.IntEnum(name, {str(value): value for value in choices})  # type: ignore # pylint: disable=invalid-name
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

    return create_model(
        "Input",
        __module__=__name__,
        __validators__=None,
        **get_input_create_model_kwargs(signature),
    )  # type: ignore


def get_output_type(predictor: BasePredictor) -> Type[BaseModel]:
    """
    Creates a Pydantic Output model from the return type annotation of a Predictor's predict() method.
    """

    predict = get_predict(predictor)
    signature = inspect.signature(predict)
    OutputType: Type[BaseModel]
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
        if PYDANTIC_V2:
            field = Field(**{"json_schema_extra": {"x-cog-array-type": "iterator"}})  # type: ignore
        else:
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
    if name == "TrainingOutput":  # pylint: disable=no-else-return

        class Output(OutputType):  # type: ignore
            pass

        return Output
    else:
        if PYDANTIC_V2:

            class Output(pydantic.RootModel[OutputType]):  # type: ignore
                pass
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

    return create_model(
        "TrainingInput",
        __module__=__name__,
        __validators__=None,
        **get_input_create_model_kwargs(signature),
    )  # type: ignore


def get_training_output_type(predictor: BasePredictor) -> Type[BaseModel]:
    """
    Creates a Pydantic Output model from the return type annotation of a train() method.
    """

    train = get_train(predictor)
    signature = inspect.signature(train)

    if signature.return_annotation is inspect.Signature.empty:
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
    else:
        TrainingOutputType = signature.return_annotation

    name = (
        TrainingOutputType.__name__ if hasattr(TrainingOutputType, "__name__") else ""
    )

    # We wrap the OutputType in a TrainingOutput class to
    # ensure consistent naming of the interface in the schema
    # See comment in get_output_type for more info.
    if name == "TrainingOutput":
        return TrainingOutputType

    if name == "Output":  # pylint: disable=no-else-return

        class TrainingOutput(TrainingOutputType):  # type: ignore
            pass

        return TrainingOutput
    else:
        if PYDANTIC_V2:

            class TrainingOutput(pydantic.RootModel[TrainingOutputType]):  # type: ignore
                pass

            return TrainingOutput

        else:

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

        if module.split(".")[0] == "cog":
            module = "cog"

        try:
            return f"{module}.{t.__qualname__}"
        except AttributeError:
            pass

    return str(t)


def readable_types_list(type_list: List[Type[Any]]) -> str:
    return ", ".join(human_readable_type_name(t) for t in type_list)
