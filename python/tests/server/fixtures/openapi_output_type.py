from cog import BasePredictor
from pydantic import BaseModel


# An output object called `Output` needs to be special cased because pydantic tries to dedupe it with the internal `Output`
class Output(BaseModel):
    foo_number: int = "42"
    foo_string: str = "meaning of life"


class Predictor(BasePredictor):
    def predict(
        self,
    ) -> Output:
        pass
