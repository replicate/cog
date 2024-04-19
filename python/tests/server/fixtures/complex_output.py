try:
    from pydantic.v1 import BaseModel  # type: ignore
except ImportError:
    from pydantic import BaseModel  # pylint: disable=W0404


class Output(BaseModel):
    number: int
    text: str


class Predictor:
    def setup(self):
        pass

    def predict(self) -> Output:
        return Output(number=42, text="meaning of life")
