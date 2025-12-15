from typing import Optional

from cog import BasePredictor, Input

ERROR = 'incompatible input type for ge/le: s: Optional[str]'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: Optional[str] = Input(ge=0)) -> str:
        pass
