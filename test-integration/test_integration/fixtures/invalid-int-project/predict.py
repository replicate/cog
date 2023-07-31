from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(
        self, num: int = Input(description="Number of things", default=1, ge=2, le=10)
    ) -> int:
        return num * 2
