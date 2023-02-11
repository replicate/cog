from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, num: int = Input(default=5)) -> int:
        return num**2
