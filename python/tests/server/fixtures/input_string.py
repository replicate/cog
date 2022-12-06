from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
