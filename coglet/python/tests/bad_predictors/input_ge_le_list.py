from typing import List

from cog import BasePredictor, Input

ERROR = 'incompatible input type for ge/le: s: List[str]'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: List[str] = Input(ge=0)) -> str:
        pass
