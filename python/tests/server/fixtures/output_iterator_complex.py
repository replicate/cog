from typing import Iterator, List

from pydantic import BaseModel

from cog import BasePredictor


class Output(BaseModel):
    text: str


class Predictor(BasePredictor):
    def predict(self) -> Iterator[List[Output]]:
        yield [Output(text="hello")]
