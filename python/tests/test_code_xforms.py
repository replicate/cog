from cog.code_xforms import strip_model_source_code


def test_strip_model_source_code():
    stripped_code = strip_model_source_code(
        """
import io

from cog import BasePredictor, Path
from typing import Optional
from pydantic import BaseModel
import torch


class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]


class Predictor(BasePredictor):
    # setup code
    def predict(self, msg: str) -> ModelOutput:
       return ModelOutput(success=False, error=msg, segmentedImage=None)
""",
        "Predictor",
        "predict",
    )
    assert (
        stripped_code
        == """from cog import BasePredictor, Path
from typing import Optional
from pydantic import BaseModel

class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]

class Predictor(BasePredictor):

    def predict(self, msg: str) -> ModelOutput:
        return None
"""
    ), "Stripped code needs to equal the minimum viable type inference."
