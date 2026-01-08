from cog import BasePredictor

ERROR = "setup() must have 'self' first argument"


class Predictor(BasePredictor):
    def setup() -> None:
        pass

    def predict(self, s: str) -> str:
        return s
