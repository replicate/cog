from cog import BasePredictor
try:
    from pydantic.v1 import BaseModel
except ImportError:
    from pydantic import BaseModel


# Calling this `MyOutput` to test if cog renames it to `Output` in the schema
class MyOutput(BaseModel):
    foo_number: int = "42"
    foo_string: str = "meaning of life"


class Predictor(BasePredictor):
    def predict(
        self,
    ) -> MyOutput:
        pass
