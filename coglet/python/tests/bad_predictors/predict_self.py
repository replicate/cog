from cog import BasePredictor

ERROR = "predict() must have 'self' first argument"


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict() -> str:
        return ''
