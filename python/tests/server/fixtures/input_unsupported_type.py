from cog import BasePredictor
try:
    from pydantic.v1 import BaseModel
except ImportError:
    from pydantic import BaseModel


class Input(BaseModel):
    text: str


class Predictor(BasePredictor):
    def predict(self, input: Input) -> str:
        return input.text
