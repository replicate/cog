from abc import ABC, abstractmethod
import importlib
import inspect
import os.path
from pathlib import Path
from typing import Literal
from pydantic import create_model

import yaml

from .errors import ConfigDoesNotExist, PredictorNotSet


# TODO(andreas): handle directory input
# TODO(andreas): handle List[Dict[str, int]], etc.
# TODO(andreas): model-level documentation


class Predictor(ABC):
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


def get_predict_types(predictor: Predictor):
    signature = inspect.signature(predictor.predict)
    create_model_kwargs = {}

    for name, parameter in signature.parameters.items():
        if not parameter.annotation:
            # TODO: perhaps should throw error if there are arguments not annotated?
            continue

        # if no default is specified, make it required with "..."
        if parameter.default is inspect.Signature.empty:
            default = ...
        else:
            default = parameter.default

        create_model_kwargs[name] = (parameter.annotation, default)

    InputType = create_model("Input", **create_model_kwargs)
    if signature.return_annotation is inspect.Signature.empty:
        OutputType = Literal[None]
    else:
        OutputType = signature.return_annotation
    return InputType, OutputType
