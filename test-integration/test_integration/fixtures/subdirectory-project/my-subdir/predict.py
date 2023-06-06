from cog import BasePredictor
from mylib import concat


class Predictor(BasePredictor):
    def predict(self, s: str) -> str:
        return concat("hello", s)
