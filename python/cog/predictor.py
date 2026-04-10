"""
Cog SDK base class definitions.

This module provides BaseRunner (the primary base class) and BasePredictor
(a backwards-compatible alias). Users subclass one of these to define their
model's prediction interface.
"""

import importlib
import importlib.util
import inspect
import os
import sys
from typing import Any, Optional, Union

from .types import Path


class BaseRunner:
    """
    Base class for Cog runners.

    Subclass this to define your model's prediction interface. Override
    the ``setup`` method to load your model, and the ``run`` method to
    run predictions.

    Example::

        from cog import BaseRunner, Input, Path

        class Runner(BaseRunner):
            def setup(self):
                self.model = load_model()

            def run(self, prompt: str = Input(description="Input text")) -> str:
                self.record_metric("temperature", 0.7)
                return self.model.generate(prompt)

    For backwards compatibility, ``BasePredictor`` is an alias for this class
    and overriding ``predict()`` instead of ``run()`` is supported.
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
            NotImplementedError: If run is not implemented by subclass.
        """
        raise NotImplementedError("run has not been implemented by subclass.")

    def predict(self, **kwargs: Any) -> Any:
        """Backwards-compatible bridge: calls ``run()``.

        Override ``run()`` instead of this method for new code.  Existing
        subclasses that override ``predict()`` will continue to work because
        the runtime detects which method was overridden and calls it directly.
        """
        return self.run(**kwargs)

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


# Backwards-compatible alias
BasePredictor = BaseRunner


def load_predictor_from_ref(ref: str) -> Any:
    """Load a predictor from a module:class reference (e.g. 'run.py:Runner')."""
    if ":" in ref:
        module_path, class_name = ref.split(":", 1)
    else:
        module_path = ref
        class_name = None  # Will try Runner, then Predictor
    module_name = os.path.basename(module_path).replace(".py", "")

    # Use spec_from_file_location to load from file path
    spec = importlib.util.spec_from_file_location(module_name, module_path)
    if spec is None or spec.loader is None:
        raise ImportError(f"Cannot load module from {module_path}")
    module = importlib.util.module_from_spec(spec)
    # Add module to sys.modules so pickle can find it
    sys.modules[module_name] = module
    spec.loader.exec_module(module)

    if class_name is None:
        # Try Runner first, fall back to Predictor
        has_runner = hasattr(module, "Runner")
        has_predictor = hasattr(module, "Predictor")
        if has_runner and has_predictor:
            import warnings

            warnings.warn(
                f"Module {module_path} defines both 'Runner' and 'Predictor'. "
                "Using 'Runner'. Specify explicitly with 'module.py:ClassName' to override.",
                stacklevel=2,
            )
            class_name = "Runner"
        elif has_runner:
            class_name = "Runner"
        elif has_predictor:
            class_name = "Predictor"
        else:
            raise ImportError(
                f"Cannot find 'Runner' or 'Predictor' class in {module_path}"
            )

    predictor = getattr(module, class_name)
    # It could be a class or a function (for training)
    if inspect.isclass(predictor):
        return predictor()
    return predictor


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
