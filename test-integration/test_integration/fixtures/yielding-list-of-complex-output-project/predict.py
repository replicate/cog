import io
from time import sleep
from typing import Iterator, List

from cog import BasePredictor, File
from pydantic import BaseModel


class Output(BaseModel):
    text: str
    file: File


class Predictor(BasePredictor):
    def predict(self) -> Iterator[List[Output]]:
        yield [Output(text="hello", file=io.StringIO("hello"))]
        sleep(0.2)  # sleep to help test timing
