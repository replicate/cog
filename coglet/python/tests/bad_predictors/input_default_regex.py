from cog import BasePredictor, Input

ERROR = "default='foo' not a regex match for input: s: str"


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str = Input(default='foo', regex='bar.*')) -> str:
        pass
