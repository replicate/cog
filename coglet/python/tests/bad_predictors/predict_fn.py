from cog import BasePredictor

ERROR = 'predict is not a function'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    predict = 0
