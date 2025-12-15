from typing import List

from cog import BasePredictor, Input

ERROR = "default=['foo'] conflicts with max_length=1 for input: s: List[str]"


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self, s: List[str] = Input(default_factory=lambda: ['foo'], max_length=1)
    ) -> str:
        pass
