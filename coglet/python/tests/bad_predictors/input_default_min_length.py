from cog import BasePredictor, Input

ERROR = "default='foo' conflicts with min_length=10 for input: s: str"


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str = Input(default='foo', min_length=10)) -> str:
        pass
