from typing import Iterator

from cog import BaseModel, BasePredictor

ERROR = 'iterator type must have a type argument'


class BadOutput(BaseModel):
    pass


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str) -> Iterator:
        yield None
