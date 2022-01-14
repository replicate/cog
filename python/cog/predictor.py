from abc import ABC, abstractmethod
from collections.abc import Generator
import enum
import importlib
import inspect
import os.path
from pathlib import Path
import typing
from pydantic import create_model, BaseModel
from pydantic.fields import FieldInfo

# Added in Python 3.8. Can be from typing if we drop support for <3.8.
from typing_extensions import Literal, get_origin, get_args
import yaml

from .errors import ConfigDoesNotExist, PredictorNotSet
from .types import Input


class BasePredictor(ABC):
    def setup(self):
        pass

    @abstractmethod
    def predict(self, **kwargs):
        pass


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


def get_input_type(predictor: BasePredictor):
    signature = inspect.signature(predictor.predict)
    create_model_kwargs = {}

    order = 0

    for name, parameter in signature.parameters.items():
        annotation = parameter.annotation

        if not annotation:
            # TODO: perhaps should throw error if there are arguments not annotated?
            continue

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
            if annotation != str:
                raise TypeError(
                    f"The input {name} uses the option choices. Choices can only be used with str types."
                )
            annotation = enum.Enum(name, {value: value for value in choices})

        create_model_kwargs[name] = (annotation, default)

    return create_model("Input", **create_model_kwargs)


def get_output_type(predictor: BasePredictor):
    signature = inspect.signature(predictor.predict)
    if signature.return_annotation is inspect.Signature.empty:
        OutputType = Literal[None]
    else:
        OutputType = signature.return_annotation

    # The type that goes in the response is the type that is yielded
    if get_origin(OutputType) is Generator:
        OutputType = get_args(OutputType)[0]

    # Wrap the type in a model so Pydantic can document it in component schema
    class Output(BaseModel):
        __root__: OutputType

    return Output
