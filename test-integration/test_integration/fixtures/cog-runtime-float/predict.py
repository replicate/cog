from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(
        self, num: float = Input(description="Number of things")
    ) -> float:
        return num * 2.0
