from typing import Any, Optional

from .types import Weights


class BasePredictor:
    def setup(
        self,
        weights: Optional[Weights] = None,  # pylint: disable=unused-argument
    ) -> None:
        """
        An optional method to prepare the model so multiple predictions run efficiently.
        """
        return

    def predict(self, **kwargs: Any) -> Any:
        """
        Run a single prediction on the model
        """
        raise NotImplementedError("predict has not been implemented by parent class.")

    def train(self, **kwargs: Any) -> Any:
        """
        Run a single train on the model
        """
        raise NotImplementedError("train has not been implemented by parent class.")
