from cog import BasePredictor

ERROR = 'setup() must not have *args'


class Predictor(BasePredictor):
    def setup(self, *args) -> None:
        pass

    def predict(self, s: str) -> str:
        return s
