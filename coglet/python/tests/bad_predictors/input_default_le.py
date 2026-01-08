from cog import BasePredictor, Input

ERROR = 'default=10 conflicts with le=0 for input: i: int'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, i: int = Input(default=10, le=0)) -> str:
        pass
