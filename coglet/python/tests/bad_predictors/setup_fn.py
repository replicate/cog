from cog import BasePredictor

ERROR = 'setup is not a function'


class Predictor(BasePredictor):
    setup = 0

    def predict(self, s: str) -> str:
        return s
