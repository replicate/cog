from abc import ABC, abstractmethod
import importlib
import inspect
import os.path
from pathlib import Path
from typing import Literal

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
    predict_types = inspect.getfullargspec(predictor.predict).annotations
    InputType = predict_types.get("input")
    OutputType = predict_types.get("return", Literal[None])
    return InputType, OutputType
