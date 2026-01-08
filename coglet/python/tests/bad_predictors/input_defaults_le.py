from typing import List

from cog import BasePredictor, Input

ERROR = 'invalid default: number must be at most 0'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self, i: List[int] = Input(default_factory=lambda: [10, 0], le=0)
    ) -> str:
        pass
