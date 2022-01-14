from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, input: int) -> int:
        return input * 2
