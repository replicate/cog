from cog import BasePredictor

ERROR = 'unexpected setup() arguments: x, y'


class Predictor(BasePredictor):
    def setup(self, x: int, y: int) -> None:
        pass

    def predict(self, s: str) -> str:
        return s
