from typing import List

from cog import BasePredictor, Input

ERROR = "default=['foo'] conflicts with min_length=10 for input: s: List[str]"


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self, s: List[str] = Input(default_factory=lambda: ['foo'], min_length=10)
    ) -> str:
        pass
