from cog import BasePredictor

from mylib import concat


class Predictor(BasePredictor):
    def predict(self, input: str) -> str:
        return concat("hello", input)
