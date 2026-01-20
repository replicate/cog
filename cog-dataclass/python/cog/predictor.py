"""
Cog SDK BasePredictor definition.

This module provides the BasePredictor class that users subclass to define
their model's prediction interface.
"""

import importlib
import inspect
import os
import types
from typing import Any, Callable, Optional, Union

from .types import Path


class BasePredictor:
    """
    Base class for Cog predictors.

    Subclass this to define your model's prediction interface. Override
    the `setup` method to load your model, and the `predict` method to
    run predictions.

    Example:
        from cog import BasePredictor, Input, Path

        class Predictor(BasePredictor):
            def setup(self):
                self.model = load_model()

            def predict(self, prompt: str = Input(description="Input text")) -> str:
                return self.model.generate(prompt)
    """

    def setup(
        self,
        weights: Optional[Union[Path, str]] = None,
    ) -> None:
        """
        Prepare the model for predictions.

        This method is called once when the predictor is initialized. Use it
        to load model weights and do any other one-time setup.

        Args:
            weights: Optional path to model weights. Can be a local path or URL.
        """
        pass

    def predict(self, **kwargs: Any) -> Any:
        """
        Run a single prediction.

        Override this method to implement your model's prediction logic.
        Input parameters should be annotated with types and optionally
        use Input() for additional metadata.

        Args:
            **kwargs: Prediction inputs as defined by the method signature.

        Returns:
            The prediction output.

        Raises:
            NotImplementedError: If predict is not implemented.
        """
        raise NotImplementedError("predict has not been implemented by parent class.")


def load_predictor_from_ref(ref: str) -> BasePredictor:
    """Load a predictor from a module:class reference."""
    module_name, class_name = ref.rsplit(":", 1) if ":" in ref else (ref, "Predictor")
    module = importlib.import_module(module_name)
    predictor_class = getattr(module, class_name)
    return predictor_class()


def get_predict(predictor: Any) -> Callable[..., Any]:
    """Get the predict method from a predictor."""
    return predictor.predict


def get_train(predictor: Any) -> Callable[..., Any]:
    """Get the train method from a predictor."""
    return predictor.train


def has_setup_weights(predictor: BasePredictor) -> bool:
    """Check if predictor's setup accepts a weights parameter."""
    if not hasattr(predictor, "setup"):
        return False
    sig = inspect.signature(predictor.setup)
    return "weights" in sig.parameters


def extract_setup_weights(predictor: BasePredictor) -> Optional[Union[Path, str]]:
    """Extract weights from environment for setup."""
    weights = os.environ.get("COG_WEIGHTS")
    if weights:
        return weights
    return None


def wait_for_env() -> None:
    """Wait for environment to be ready (noop in dataclass version)."""
    pass
