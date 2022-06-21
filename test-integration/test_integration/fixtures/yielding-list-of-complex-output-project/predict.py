import io
from typing import Iterator, List

from cog import BasePredictor, File
from pydantic import BaseModel


class Output(BaseModel):
    text: str
    file: File


class Predictor(BasePredictor):
    def predict(self) -> Iterator[List[Output]]:
        yield [Output(text="hello", file=io.StringIO("hello"))]
