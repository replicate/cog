from cog import BasePredictor, Input
from typing import Literal


class Predictor(BasePredictor):
    def predict(self, x: Literal[1, 2]) -> int:
        return x**2
