from cog import BasePredictor

ERROR = 'predict() must not return None'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str) -> None:
        pass
