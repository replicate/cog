from cog import BasePredictor, Input
from typing import Optional


class Predictor(BasePredictor):
    def predict(self, s: Optional[str] = Input(description="Hello String.")) -> str:
        return "hello " + (s if s is not None else "No One")
