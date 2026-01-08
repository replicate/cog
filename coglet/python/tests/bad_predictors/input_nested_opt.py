from typing import List, Optional

from cog import BasePredictor

ERROR = 'invalid input field opt: Optional cannot have nested type list'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, opt: Optional[List[int]]) -> str:
        pass
