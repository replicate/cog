from cog import BasePredictor, Input

ERROR = 'incompatible input type for ge/le: s: str'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str = Input(ge=0)) -> str:
        pass
