from abc import ABC, abstractmethod
import ast
from collections.abc import Iterator
import enum
import importlib.util
import inspect
import io
import os.path
from pathlib import Path
from pydantic import create_model, BaseModel, Field
from pydantic.fields import FieldInfo
from typing import Any, Callable, Dict, List, Optional, Type, Union

# Added in Python 3.8. Can be from typing if we drop support for <3.8.
from typing_extensions import get_origin, get_args, Annotated
import yaml

from .errors import ConfigDoesNotExist, PredictorNotSet
from .types import (
    Input,
    Path as CogPath,
    File as CogFile,
    URLFile,
    URLPath,
    get_filename,
)


ALLOWED_INPUT_TYPES = [str, int, float, bool, CogFile, CogPath]


class BasePredictor(ABC):
    def setup(self, weights: Optional[Union[CogFile, CogPath]] = None) -> None:
        """
        An optional method to prepare the model so multiple predictions run efficiently.
        """

    @abstractmethod
    def predict(self, **kwargs: Any) -> Any:
        """
        Run a single prediction on the model
        """


def run_setup(predictor: BasePredictor) -> None:
    weights_type = get_weights_type(predictor.setup)

    # No weights need to be passed, so just run setup() without any arguments.
    if weights_type is None:
        predictor.setup()
        return

    weights: Union[io.IOBase, Path, None]

    weights_url = os.environ.get("COG_WEIGHTS")
    weights_path = "weights"

    # TODO: Cog{File,Path}.validate(...) methods accept either "real"
    # paths/files or URLs to those things. In future we can probably tidy this
    # up a little bit.
    if weights_url:
        if weights_type == CogFile:
            weights = CogFile.validate(weights_url)
        elif weights_type == CogPath:
            weights = CogPath.validate(weights_url)
        else:
            raise ValueError(
                f"Predictor.setup() has an argument 'weights' of type {weights_type}, but only File and Path are supported"
            )
    elif os.path.exists(weights_path):
        if weights_type == CogFile:
            weights = open(weights_path, "rb")
        elif weights_type == CogPath:
            weights = CogPath(weights_path)
        else:
            raise ValueError(
                f"Predictor.setup() has an argument 'weights' of type {weights_type}, but only File and Path are supported"
            )
    else:
        weights = None

    predictor.setup(weights=weights)


def get_weights_type(setup_function) -> Optional[Any]:
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


def get_predictor_ref(config: Dict[str, Any], mode: str = "predict") -> str:
    if mode not in ["predict", "train"]:
        raise ValueError(f"Invalid mode: {mode}")

    if mode not in config:
        raise PredictorNotSet(
            f"Can't run predictions: '{mode}' option not found in cog.yaml"
        )

    return config[mode]


def load_predictor_from_ref(ref: str) -> BasePredictor:
    module_path, class_name = ref.split(":", 1)
    module_name = os.path.basename(module_path).split(".py", 1)[0]
    spec = importlib.util.spec_from_file_location(module_name, module_path)
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    predictor = getattr(module, class_name)
    # It could be a class or a function
    if inspect.isclass(predictor):
        return predictor()
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
            if isinstance(value, URLPath):
                value.unlink()
            # Note this is pathlib.Path, which cog.Path is a subclass of. A pathlib.Path object shouldn't make its way here,
            # but both have an unlink() method, so may as well be safe.
            elif isinstance(value, Path):
                # This could be missing_ok=True when we drop support for Python 3.7
                if value.exists():
                    value.unlink()


def get_predict(predictor):
    if hasattr(predictor, "predict"):
        return predictor.predict
    return predictor

class PredictVisitor(ast.NodeVisitor):
    """
    Parses a Python file to find the cog predictor class, then finds the predict() method and parses its arguments to create a Pydantic Input model.
    """
    def __init__(self, class_name: str) -> None:
        self.class_name = class_name
        # I don't love this -  but it's the easiest way to map from the string representation of a type to the actual type
        self.type_str_to_type = {'str': str, 'int': int, 'float': float, 'bool': bool, 'File': CogFile, 'Path': CogPath}


    def visit_ClassDef(self, node) -> Type[BaseInput]:
        """
        overwrites a "visit_ClassDef" method from the ast.NodeVisitor class - visits all class definitions, and if the class name matches the one we're looking for, calls the parse_predict method
        """
        print('Class:', node.name)
        if node.name == self.class_name:
            print("Found predictor class")
        for body_node in node.body:
            if isinstance(body_node, ast.FunctionDef) and body_node.name == "predict":
                self.parse_predict(body_node)


    def parse_predict(self, node) -> Type[BaseInput]:
        """
        Parses predict method without loading any code. wild. 
        """
        print('Function:', node.name)
        create_model_kwargs = {}
        order = 0

        # The defaults list is aligned from the end, so reverse both lists for easy alignment
        reversed_args = list(reversed(node.args.args))
        reversed_defaults = list(reversed(node.args.defaults))

        for i, arg in enumerate(reversed_args):
            print('Argument:', arg.arg)
            if arg.arg == 'self':
                continue
            if not arg.annotation:
                raise TypeError(
                    f"No input type provided for parameter `{arg.arg}`. Supported input types are: {list(self.type_str_to_type.keys())}."
                )
            elif ast.unparse(arg.annotation) not in self.type_str_to_type:
                raise TypeError(
                    f"Unsupported input type {ast.unparse(arg.annotation)} for parameter `{arg.arg}`. Supported input types are: {list(self.type_str_to_type.keys())}."
                )
            InputType= self.type_str_to_type[ast.unparse(arg.annotation)]
            
            # Do we actually still support not having a default? 
            if i < len(reversed_defaults):
                default = reversed_defaults[i]
                if isinstance(default, ast.Call) and isinstance(default.func, ast.Name) and default.func.id == 'Input':
                    # print('        Input args:')
                    # this is probably a task for chatgpt again. 
                    kwargs = {}
                    for keyword in default.keywords:
                        kwargs[keyword.arg] = ast.literal_eval(keyword.value)
                        #print('            ', keyword.arg, ':', ast.unparse(keyword.value))
                        default = Input(**kwargs)
                else:
                    # If user hasn't used `Input`, then wrap it in that
                    default = Input(default=ast.literal_eval(default))
            else:
                # if no default is specified, create an empty, required input
                default = Input()

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
                        arg.arg, {value: value for value in choices}
                    )
                elif InputType == int:
                    InputType = enum.IntEnum(arg.arg, {str(value): value for value in choices})  # type: ignore
                else:
                    raise TypeError(
                        f"The input {arg.arg} uses the option choices. Choices can only be used with str or int types."
                    )

            create_model_kwargs[arg.arg] = (InputType, default)

        print(create_model_kwargs)
        return create_model(
        "Input",
        __config__=None,
        __base__=BaseInput,
        __module__=__name__,
        __validators__=None,
        **create_model_kwargs,
    )


def get_input_type(predict_module: str, predict_object: str) -> Type[BaseInput]:
    with open(predict_module, 'r') as file:
        content = file.read()
    module = ast.parse(content)
    visitor = PredictVisitor(predict_object)
    return visitor.visit(module)


def get_output_type(predictor: BasePredictor) -> Type[BaseModel]:
    """
    Creates a Pydantic Output model from the return type annotation of a Predictor's predict() method.
    """
    predict = get_predict(predictor)
    signature = inspect.signature(predict)
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
