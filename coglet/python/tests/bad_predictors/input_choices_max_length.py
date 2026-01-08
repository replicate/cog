from cog import BasePredictor, Input

ERROR = 'choices and min_length/max_length are mutually exclusive: s: str'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str = Input(choices=['a', 'b'], max_length=10)) -> str:
        pass
