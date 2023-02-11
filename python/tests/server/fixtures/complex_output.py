from pydantic import BaseModel


class Output(BaseModel):
    number: int
    text: str


class Predictor:
    def setup(self):
        pass

    def predict(self) -> Output:
        return Output(number=42, text="meaning of life")
