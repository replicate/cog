from cog import BasePredictor, Input

ERROR = 'incompatible input type for choices: b: bool'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, b: bool = Input(choices=['a', 'b'])) -> str:
        pass
