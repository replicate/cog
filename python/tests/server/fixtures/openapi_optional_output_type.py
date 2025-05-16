from typing import Optional, Union

from cog import BasePredictor
from pydantic import BaseModel


class ModelOutput(BaseModel):
    foo_number: int = "42"
    foo_string: Optional[str] = None


class Predictor(BasePredictor):
    def predict(
        self,
    ) -> Union[ModelOutput, Optional[str]]:
        pass
