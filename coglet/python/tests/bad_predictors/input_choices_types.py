from cog import BasePredictor, Input

ERROR = 'not all choices have the same type as input: s: str'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str = Input(choices=['a', 0])) -> str:
        pass
