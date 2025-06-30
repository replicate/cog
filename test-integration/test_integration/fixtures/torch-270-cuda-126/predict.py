from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, s: str) -> str:
        return "hello " + s
