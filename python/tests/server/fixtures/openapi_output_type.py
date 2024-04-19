from cog import BasePredictor

try:
    from pydantic.v1 import BaseModel  # type: ignore
except ImportError:
    from pydantic import BaseModel  # pylint: disable=W0404
    

# An output object called `Output` needs to be special cased because pydantic tries to dedupe it with the internal `Output`
class Output(BaseModel):
    foo_number: int = "42"
    foo_string: str = "meaning of life"


class Predictor(BasePredictor):
    def predict(
        self,
    ) -> Output:
        pass
