from cog import BasePredictor, Input

ERROR = 'default=0 conflicts with ge=10 for input: i: int'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, i: int = Input(default=0, ge=10)) -> str:
        pass
