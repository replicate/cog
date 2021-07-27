from abc import ABC, abstractmethod
import importlib
import os.path
from pathlib import Path
from typing import Dict, Any

import yaml

from .errors import ConfigDoesNotExist, ModelNotSet


# TODO(andreas): handle directory input
# TODO(andreas): handle List[Dict[str, int]], etc.
# TODO(andreas): model-level documentation


class Model(ABC):
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

        inputs = {}
        if hasattr(self.predict, "_inputs"):
            input_specs = self.predict._inputs
            for name, spec in input_specs.items():
                arg: Dict[str, Any] = {
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
                inputs[name] = arg
        return {"inputs": inputs}


def run_model(model, inputs, cleanup_functions):
    """
    Run the model on the inputs, and append resulting paths
    to cleanup functions for removal.
    """
    result = model.predict(**inputs)
    if isinstance(result, Path):
        cleanup_functions.append(result.unlink)
    return result


def load_model():
    # Assumes the working directory is /src
    config_path = os.path.abspath("cog.yaml")
    try:
        with open(config_path) as fh:
            config = yaml.safe_load(fh)
    except FileNotFoundError:
        raise ConfigDoesNotExist(
            f"Could not find {config_path}",
        )

    if "model" not in config:
        raise ModelNotSet("Can't run predictions: 'model' option not found in cog.yaml")

    # TODO: handle predict scripts in subdirectories
    model = config["model"]
    module_name, class_name = model.split(".py:", 1)
    module = importlib.import_module(module_name)
    model_class = getattr(module, class_name)
    return model_class()
