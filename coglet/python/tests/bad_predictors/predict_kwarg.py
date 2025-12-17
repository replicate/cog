from cog import BasePredictor

ERROR = 'predict() must not have **kwargs'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, **kwargs) -> None:
        pass
