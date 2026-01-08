from cog import BasePredictor

ERROR = 'predict() must not have *args'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, *args) -> None:
        pass
