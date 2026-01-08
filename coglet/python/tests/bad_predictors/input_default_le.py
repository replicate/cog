from cog import BasePredictor, Input

ERROR = 'invalid default: number must be at most 0'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, i: int = Input(default=10, le=0)) -> str:
        pass
