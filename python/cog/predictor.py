from abc import ABC, abstractmethod
import importlib
import os.path
from pathlib import Path
from typing import Dict, Any

import yaml

from .errors import ConfigDoesNotExist, PredictorNotSet


# TODO(andreas): handle directory input
# TODO(andreas): handle List[Dict[str, int]], etc.
# TODO(andreas): model-level documentation


class Predictor(ABC):
    @abstractmethod
    def setup(self):
        pass

    @abstractmethod
    def predict(self, **kwargs):
        pass

    def get_type_signature(self):
        """
        Returns a dict describing the inputs of the model.
        """
        from .input import (
            get_type_name,
            UNSPECIFIED,
        )

        inputs = []
        if hasattr(self.predict, "_inputs"):
            input_specs = self.predict._inputs
            for spec in input_specs:
                arg: Dict[str, Any] = {
                    "name": spec.name,
                    "type": get_type_name(spec.type),
                }
                if spec.help:
                    arg["help"] = spec.help
                if spec.default is not UNSPECIFIED:
                    arg["default"] = str(spec.default)  # TODO: don't string this
                if spec.min is not None:
                    arg["min"] = str(spec.min)  # TODO: don't string this
                if spec.max is not None:
                    arg["max"] = str(spec.max)  # TODO: don't string this
                if spec.options is not None:
                    arg["options"] = [str(o) for o in spec.options]
                inputs.append(arg)
        return {"inputs": inputs}


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

    # TODO: handle predict scripts in subdirectories
    predict_string = config["predict"]
    module_name, class_name = predict_string.split(".py:", 1)
    module = importlib.import_module(module_name)
    predictor_class = getattr(module, class_name)
    return predictor_class()
