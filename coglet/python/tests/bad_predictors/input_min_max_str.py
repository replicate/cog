from cog import BasePredictor, Input

ERROR = 'incompatible input type for min_length/max_length: i: int'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, i: int = Input(min_length=0)) -> str:
        pass
