from cog import BasePredictor


class Predictor(BasePredictor):
    test_inputs = {"s": "world"}

    def predict(self, s: str) -> str:
        return "hello " + s
