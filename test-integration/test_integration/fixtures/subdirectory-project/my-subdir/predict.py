import cog

from mylib import concat


class Predictor(cog.Predictor):
    def predict(self, input: str) -> str:
        return concat("hello", input)
