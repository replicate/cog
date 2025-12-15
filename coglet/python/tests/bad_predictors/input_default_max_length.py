from cog import BasePredictor, Input

ERROR = "default='foo' conflicts with max_length=1 for input: s: str"


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str = Input(default='foo', max_length=1)) -> str:
        pass
