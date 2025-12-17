from cog import BasePredictor

ERROR = 'setup() must not have **kwargs'


class Predictor(BasePredictor):
    def setup(self, **kwargs) -> None:
        pass

    def predict(self, s: str) -> str:
        return s
