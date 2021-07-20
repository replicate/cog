from abc import ABC, abstractmethod
from pathlib import Path


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
