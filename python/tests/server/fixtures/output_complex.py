import io

from cog import BasePredictor, File

try:
    from pydantic.v1 import BaseModel  # type: ignore
except ImportError:
    from pydantic import BaseModel  # pylint: disable=W0404


class Output(BaseModel):
    text: str
    file: File


class Predictor(BasePredictor):
    def predict(self) -> Output:
        return Output(text="hello", file=io.StringIO("hello"))
