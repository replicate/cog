from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, input: str) -> str:
        return "hello " + input
