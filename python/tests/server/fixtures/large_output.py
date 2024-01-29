from typing import List

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(
        self, n: int = 2**20
    ) -> List[int]:
        return [1] * n
