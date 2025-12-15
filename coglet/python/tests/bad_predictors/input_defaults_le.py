from typing import List

from cog import BasePredictor, Input

ERROR = 'default=[10, 0] conflicts with le=0 for input: i: List[int]'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self, i: List[int] = Input(default_factory=lambda: [10, 0], le=0)
    ) -> str:
        pass
