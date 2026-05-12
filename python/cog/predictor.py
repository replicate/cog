"""
Cog SDK BasePredictor definition.

This module provides the BasePredictor class that users subclass to define
their model's prediction interface.
"""

import importlib
import importlib.util
import inspect
import os
import sys
import warnings
from typing import Any, Optional, Union

from .types import Path


class BaseRunner:
    """
    Base class for Cog runners.

    Subclass this to define your model's run interface. Override the `setup`
    method to load your model, and the `run` method to execute it.

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
        Prepare the model for runs.

        This method is called once when the runner is initialized. Use it
        to load model weights and do any other one-time setup.

        Args:
            weights: Optional path to model weights. Can be a local path or URL.
        """
        pass

    def run(self, *args: Any, **kwargs: Any) -> Any:
        """
        Run the model once.

        Override this method to implement your model's prediction logic.
        Input parameters should be annotated with types and optionally
        use Input() for additional metadata.

        Args:
            *args: Positional run inputs as defined by the method signature.
            **kwargs: Keyword run inputs as defined by the method signature.

        Returns:
            The prediction output.

        Raises:
            NotImplementedError: If run is not implemented.
        """
        run_owner = _user_method_owner(self.__class__, "run")
        predict_owner = _user_method_owner(self.__class__, "predict")
        if predict_owner is not None and run_owner is None:
            return self.predict(*args, **kwargs)
        raise NotImplementedError("run has not been implemented by parent class.")

    def predict(self, *args: Any, **kwargs: Any) -> Any:
        """Deprecated compatibility bridge to run()."""
        return self.run(*args, **kwargs)

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
            key: Metric name. Each segment must start with a letter and contain
                only letters, digits, and underscores (no leading/trailing/consecutive
                underscores). Dots create nested objects (e.g. ``"timing.inference"``).
                Maximum 128 characters, 4 segments. Reserved: ``"predict_time"`` and
                anything starting with ``"cog."``.
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


class BasePredictor(BaseRunner):
    """Deprecated compatibility alias for BaseRunner."""


def _user_method_owner(cls: type[Any], method_name: str) -> type[Any] | None:
    for owner in inspect.getmro(cls):
        if owner in {BaseRunner, BasePredictor, object}:
            break
        value = owner.__dict__.get(method_name)
        if callable(value):
            return owner
    return None


def _validate_runner_class(cls: type[Any], class_name: str) -> None:
    run_owner = _user_method_owner(cls, "run")
    predict_owner = _user_method_owner(cls, "predict")
    defines_run = run_owner is not None
    defines_predict = predict_owner is not None
    if defines_run and defines_predict:
        raise ValueError(
            f"{class_name} must define either run() or predict(), not both"
        )
    if not defines_run and not defines_predict:
        raise ValueError(f"run or predict method not found: {class_name}")
    if defines_predict:
        warnings.warn(
            f"{class_name}.predict() is deprecated; use run() instead",
            DeprecationWarning,
            stacklevel=3,
        )
    if any(base is BasePredictor for base in inspect.getmro(cls)[1:]):
        warnings.warn(
            "BasePredictor is deprecated; use BaseRunner instead",
            DeprecationWarning,
            stacklevel=3,
        )


def load_predictor_from_ref(ref: str) -> BaseRunner:
    """Load a predictor from a module:class reference (e.g. 'predict.py:Predictor')."""
    module_path, explicit_class_name = ref.split(":", 1) if ":" in ref else (ref, None)
    module_name = os.path.basename(module_path).replace(".py", "")

    # Use spec_from_file_location to load from file path
    spec = importlib.util.spec_from_file_location(module_name, module_path)
    if spec is None or spec.loader is None:
        raise ImportError(f"Cannot load module from {module_path}")
    module = importlib.util.module_from_spec(spec)
    # Add module to sys.modules so pickle can find it
    sys.modules[module_name] = module
    spec.loader.exec_module(module)

    if explicit_class_name is None:
        if hasattr(module, "Runner"):
            if hasattr(module, "Predictor"):
                warnings.warn(
                    "Both Runner and Predictor are defined; using Runner. Specify a class "
                    "name explicitly if this is not intended.",
                    UserWarning,
                    stacklevel=2,
                )
            class_name = "Runner"
        elif hasattr(module, "Predictor"):
            warnings.warn(
                "Predictor is deprecated; use Runner instead",
                DeprecationWarning,
                stacklevel=2,
            )
            class_name = "Predictor"
        else:
            raise AttributeError(f"module {module_name!r} has no Runner or Predictor")
    else:
        class_name = explicit_class_name

    predictor = getattr(module, class_name)
    # It could be a class or a function (for training)
    if inspect.isclass(predictor):
        _validate_runner_class(predictor, class_name)
        return predictor()
    return predictor


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
