from pydantic import BaseModel

from cog import BasePredictor


class Outer(BaseModel):
    inner: str


class Predictor(BasePredictor):
    def predict(self, outer: Outer) -> str:
        return outer.inner
