from typing import Iterator, List

from cog import BasePredictor

try:
    from pydantic.v1 import BaseModel  # type: ignore
except ImportError:
    from pydantic import BaseModel  # pylint: disable=W0404


class Output(BaseModel):
    text: str


class Predictor(BasePredictor):
    def predict(self) -> Iterator[List[Output]]:
        yield [Output(text="hello")]
