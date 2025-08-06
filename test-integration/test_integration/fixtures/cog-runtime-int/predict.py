from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(
        self, num: int = Input(description="Number of things")
    ) -> int:
        return num * 2
