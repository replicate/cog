from typing import List

from cog import BasePredictor
from pydantic import BaseModel


class Output(BaseModel):
    foo: str


class Predictor(BasePredictor):
    def predict(
        self,
    ) -> List[Output]:
        pass
