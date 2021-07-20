from abc import ABC, abstractmethod
import importlib
from pathlib import Path

import yaml


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
    with open("cog.yaml") as fh:
        config = yaml.safe_load(fh)

    if "model" not in config:
        raise Exception("Can't run predictions: 'model' option not found in cog.yaml")

    # TODO: handle predict scripts in subdirectories
    model = config["model"]
    module_name, class_name = model.split(".py:", 1)
    module = importlib.import_module(module_name)
    model_class = getattr(module, class_name)
    return model_class()
