from typing import List

from cog import BasePredictor, Input

ERROR = 'default=[0, 100] conflicts with ge=10 for input: i: List[int]'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self, i: List[int] = Input(default_factory=lambda: [0, 100], ge=10)
    ) -> str:
        pass
