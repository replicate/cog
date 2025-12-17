from typing import List

from cog import BasePredictor

ERROR = 'invalid input field xs: List cannot have nested type list'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, xs: List[List[int]]) -> str:
        pass
