import io

from cog import BasePredictor, Path
from typing import Optional
try:
    from pydantic.v1 import BaseModel
except ImportError:
    from pydantic import BaseModel



class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]


class Predictor(BasePredictor):
    # setup code
    def predict(self, msg: str) -> ModelOutput:
       return ModelOutput(success=False, error=msg, segmentedImage=None)
