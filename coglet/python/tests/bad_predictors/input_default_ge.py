from cog import BasePredictor, Input

ERROR = 'invalid default: number must be at least 10'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, i: int = Input(default=0, ge=10)) -> str:
        pass
