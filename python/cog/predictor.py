"""
Cog SDK BaseRunner (and legacy BasePredictor) definition.

This module provides the BaseRunner class that users subclass to define
their model's prediction interface. BasePredictor is kept as an alias for
backwards compatibility.
"""

import importlib
import importlib.util
import inspect
import os
import sys
from typing import Any, Callable, Optional, Union

from .types import Path


class BaseRunner:
    """
    Base class for Cog runners.

    Subclass this to define your model's prediction interface. Override
    the `setup` method to load your model, and the `run` method to
    run predictions.

    Example:
        from cog import BaseRunner, Input, Path

        class Runner(BaseRunner):
            def setup(self):
                self.model = load_model()

            def run(self, prompt: str = Input(description="Input text")) -> str:
                self.record_metric("temperature", 0.7)
                return self.model.generate(prompt)
    """

    def setup(
        self,
        weights: Optional[Union[Path, str]] = None,
    ) -> None:
        """
        Prepare the model for predictions.

        This method is called once when the runner is initialized. Use it
        to load model weights and do any other one-time setup.

        Args:
            weights: Optional path to model weights. Can be a local path or URL.
        """
        pass

    def run(self, **kwargs: Any) -> Any:
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
            NotImplementedError: If run is not implemented.
        """
        raise NotImplementedError("run has not been implemented by subclass.")

    def predict(self, **kwargs: Any) -> Any:
        """
        Legacy alias for run().

        Override `run` instead. If only `predict` is overridden (for backwards
        compatibility), the framework will call it automatically.
        """
        raise NotImplementedError("predict has not been implemented by subclass.")

    @property
    def scope(self) -> Any:
        """The current prediction scope.

        Provides access to the full scope API for advanced metric operations
        like dict-style access and deletion::

            self.scope.metrics["token_count"] = 42
            del self.scope.metrics["token_count"]

        Outside an active prediction this returns a no-op scope.
        """
        import coglet

        return coglet._sdk.current_scope()  # type: ignore[attr-defined]

    def record_metric(self, key: str, value: Any, mode: str = "replace") -> None:
        """Record a prediction metric.

        Convenience method for recording metrics on the current prediction
        scope. Outside an active prediction this is a silent no-op.

        Args:
            key: Metric name. Use dot-separated keys (e.g. ``"timing.inference"``)
                to create nested objects in the metrics output.
            value: Metric value. Supported types: bool, int, float, str, list, dict.
                Setting a value to ``None`` deletes the metric.
            mode: Accumulation mode. One of:
                - ``"replace"`` (default): overwrite any previous value.
                - ``"incr"``: add to the existing numeric value.
                - ``"append"``: append to an array.

        Example::

            class Runner(BaseRunner):
                def run(self, prompt: str) -> str:
                    self.record_metric("temperature", 0.7)
                    self.record_metric("token_count", 1, mode="incr")
                    return self.model.generate(prompt)
        """
        self.scope.record_metric(key, value, mode=mode)


# Legacy alias for backwards compatibility
BasePredictor = BaseRunner


def _has_custom_run(obj: Any) -> bool:
    """Check if the object has a user-defined run() method (not the base class default)."""
    if not hasattr(obj, "run"):
        return False
    # Check if run() is overridden from the BaseRunner default
    method = (
        getattr(type(obj), "run", None)
        if inspect.isclass(type(obj))
        else getattr(obj, "run", None)
    )
    return method is not None and method is not BaseRunner.run


def _has_custom_predict(obj: Any) -> bool:
    """Check if the object has a user-defined predict() method (not the base class default)."""
    if not hasattr(obj, "predict"):
        return False
    method = (
        getattr(type(obj), "predict", None)
        if inspect.isclass(type(obj))
        else getattr(obj, "predict", None)
    )
    return method is not None and method is not BaseRunner.predict


def load_predictor_from_ref(ref: str) -> BaseRunner:
    """Load a runner from a module:class reference (e.g. 'run.py:Runner')."""
    module_path, class_name = ref.split(":", 1) if ":" in ref else (ref, "Predictor")
    module_name = os.path.basename(module_path).replace(".py", "")

    # Use spec_from_file_location to load from file path
    spec = importlib.util.spec_from_file_location(module_name, module_path)
    if spec is None or spec.loader is None:
        raise ImportError(f"Cannot load module from {module_path}")
    module = importlib.util.module_from_spec(spec)
    # Add module to sys.modules so pickle can find it
    sys.modules[module_name] = module
    spec.loader.exec_module(module)

    predictor = getattr(module, class_name)
    # It could be a class or a function (for training)
    if inspect.isclass(predictor):
        return predictor()
    return predictor


def get_predict(predictor: Any) -> Callable[..., Any]:
    """Get the run/predict method from a runner.

    Prefers `run()` if defined, falls back to `predict()` for backwards
    compatibility. If both are overridden, raises an error.
    """
    # If predictor is a function, return it directly
    if (
        callable(predictor)
        and not inspect.isclass(predictor)
        and not hasattr(predictor, "predict")
        and not hasattr(predictor, "run")
    ):
        return predictor

    has_run = _has_custom_run(predictor)
    has_predict = _has_custom_predict(predictor)

    if has_run and has_predict:
        raise ValueError(
            "Cannot define both run() and predict() on the same class. "
            "Use run() (predict() is deprecated)."
        )

    if has_run:
        return predictor.run
    # Fall back to predict() for backwards compatibility
    return predictor.predict


def get_train(predictor: Any) -> Callable[..., Any]:
    """Get the train method from a predictor."""
    # If predictor is a function (not a class instance), return it directly
    if (
        callable(predictor)
        and not inspect.isclass(predictor)
        and not hasattr(predictor, "train")
    ):
        return predictor
    return predictor.train


def has_setup_weights(predictor: BaseRunner) -> bool:
    """Check if predictor's setup accepts a weights parameter."""
    if not hasattr(predictor, "setup"):
        return False
    sig = inspect.signature(predictor.setup)
    return "weights" in sig.parameters


def extract_setup_weights(predictor: BaseRunner) -> Optional[Union[Path, str]]:
    """Extract weights from environment for setup."""
    weights = os.environ.get("COG_WEIGHTS")
    if weights:
        return weights
    return None


def wait_for_env() -> None:
    """Wait for environment to be ready (noop in dataclass version)."""
    pass


def get_healthcheck(predictor: Any) -> Optional[Callable[[], bool]]:
    """Get the healthcheck method from a predictor if it exists."""
    if hasattr(predictor, "healthcheck"):
        return predictor.healthcheck
    return None
