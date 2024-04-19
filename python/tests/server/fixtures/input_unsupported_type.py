from cog import BasePredictor

try:
    from pydantic.v1 import BaseModel  # type: ignore
except ImportError:
    from pydantic import BaseModel  # pylint: disable=W0404
    

class Input(BaseModel):
    text: str


class Predictor(BasePredictor):
    def predict(self, input: Input) -> str:
        return input.text
