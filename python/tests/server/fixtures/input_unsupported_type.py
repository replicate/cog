from cog import BasePredictor
from pydantic import BaseModel


class Input(BaseModel):
    text: str


class Predictor(BasePredictor):
    def predict(self, input: Input) -> str:
        return input.text
