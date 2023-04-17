import io

from cog._vendor.pydantic import BaseModel

from cog import BasePredictor, File


class Output(BaseModel):
    text: str
    file: File


class Predictor(BasePredictor):
    def predict(self) -> Output:
        return Output(text="hello", file=io.StringIO("hello"))
