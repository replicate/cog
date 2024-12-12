from abc import ABC, abstractmethod
from typing import Any, Optional

from .types import Weights


class BasePredictor(ABC):
    def setup(
        self,
        weights: Optional[Weights] = None,  # pylint: disable=unused-argument
    ) -> None:
        """
        An optional method to prepare the model so multiple predictions run efficiently.
        """
        return

    @abstractmethod
    def predict(self, **kwargs: Any) -> Any:
        """
        Run a single prediction on the model
        """
