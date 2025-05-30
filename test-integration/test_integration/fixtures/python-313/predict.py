from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, num: int) -> int:
        return num * 2
