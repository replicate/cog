from cog import BasePredictor, Input

ERROR = "choices=['a'] must have >= 2 elements: s: str"


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str = Input(choices=['a'])) -> str:
        pass
