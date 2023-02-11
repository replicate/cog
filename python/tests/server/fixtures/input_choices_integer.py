from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, x: int = Input(choices=[1, 2])) -> int:
        return x**2
