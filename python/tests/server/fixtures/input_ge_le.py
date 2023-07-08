from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, num: float = Input(ge=3.01, le=10.5)) -> float:
        return num
