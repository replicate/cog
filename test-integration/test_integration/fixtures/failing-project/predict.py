from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        raise Exception("over budget")
