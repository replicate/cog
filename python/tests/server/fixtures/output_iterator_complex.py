from typing import Iterator, List

from cog import BasePredictor
try:
    from pydantic.v1 import BaseModel
except ImportError:
    from pydantic import BaseModel


class Output(BaseModel):
    text: str


class Predictor(BasePredictor):
    def predict(self) -> Iterator[List[Output]]:
        yield [Output(text="hello")]
