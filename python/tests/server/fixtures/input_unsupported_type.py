from pydantic import BaseModel
from cog import BasePredictor


class Input(BaseModel):
    text: str


class Predictor(BasePredictor):
    def predict(self, input: Input) -> str:
        return input.text
