from cog import BasePredictor, Input
from typing import Literal


class Predictor(BasePredictor):
    def predict(self, text: Literal["foo", "bar"]) -> str:
        assert type(text) == str
        return text
