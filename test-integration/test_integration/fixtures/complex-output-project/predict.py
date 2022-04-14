from pydantic import BaseModel

from cog import BasePredictor


class Output(BaseModel):
    hello: str
    goodbye: str


class Predictor(BasePredictor):
    def predict(self, name: str) -> Output:
        return Output(
            hello="hello " + name,
            goodbye="goodbye " + name,
        )
