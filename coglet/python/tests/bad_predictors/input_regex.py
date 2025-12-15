from cog import BasePredictor, Input

ERROR = 'incompatible input type for regex: i: int'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, i: int = Input(default=0, regex='bar.*')) -> str:
        pass
