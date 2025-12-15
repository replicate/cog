from cog import BasePredictor, Input

ERROR = 'choices and ge/le are mutually exclusive: i: int'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, i: int = Input(choices=[0, 1], ge=0)) -> str:
        pass
