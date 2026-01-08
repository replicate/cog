from cog import BasePredictor

ERROR = 'setup() must return None'


class Predictor(BasePredictor):
    def setup(self) -> int:
        return 0

    def predict(self, s: str) -> str:
        return s
