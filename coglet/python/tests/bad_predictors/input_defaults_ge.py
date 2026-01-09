from typing import List

from cog import BasePredictor, Input

ERROR = 'invalid default: number must be at least 10'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self, i: List[int] = Input(default_factory=lambda: [0, 100], ge=10)
    ) -> str:
        pass
