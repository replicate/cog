import os
import sys
import uuid

import pytest

from cog.code_xforms import load_module_from_string, strip_model_source_code

g_module_dir = os.path.dirname(os.path.abspath(__file__))


@pytest.mark.skipif(sys.version_info < (3, 9), reason="requires python3.9 or higher")
def test_train_function_model():
    with open(f"{g_module_dir}/server/fixtures/train.py", encoding="utf-8") as file:
        source_code = file.read()

    new_source = strip_model_source_code(source_code, "train", "train")
    expected_source = """
from cog import BaseModel, Input, Path
class TrainingOutput(BaseModel):
    weights: Path
def train(n: int=Input(description='Dimension of weights to generate')) -> TrainingOutput:
    return None
"""
    assert expected_source.strip() == new_source.strip()
    assert load_module_from_string(uuid.uuid4().hex, new_source)


@pytest.mark.skipif(sys.version_info < (3, 9), reason="requires python3.9 or higher")
def test_predict_many_inputs():
    with open(
        f"{g_module_dir}/../../test-integration/test_integration/fixtures/many-inputs-project/predict.py",
        encoding="utf-8",
    ) as file:
        source_code = file.read()

    new_source = strip_model_source_code(source_code, ["Predictor"], ["predict"])
    expected_source = """
from cog import BasePredictor, Input, Path
class Predictor(BasePredictor):

    def predict(self, no_default: str, default_without_input: str='default', input_with_default: int=Input(default=10), path: Path=Input(description='Some path'), image: Path=Input(description='Some path'), choices: str=Input(choices=['foo', 'bar']), int_choices: int=Input(description='hello', choices=[3, 4, 5])) -> str:
        return None
"""
    assert expected_source.strip() == new_source.strip()
    assert load_module_from_string(uuid.uuid4().hex, new_source)


@pytest.mark.skipif(sys.version_info < (3, 9), reason="requires python3.9 or higher")
def test_predict_output_path_model():
    with open(
        f"{g_module_dir}/../../test-integration/test_integration/fixtures/path-output-project/predict.py",
        encoding="utf-8",
    ) as file:
        source_code = file.read()

    new_source = strip_model_source_code(source_code, ["Predictor"], ["predict"])
    expected_source = """
import os
from cog import BasePredictor, Path
class Predictor(BasePredictor):

    def predict(self) -> Path:
        return None
"""
    assert expected_source.strip() == new_source.strip()
    assert load_module_from_string(uuid.uuid4().hex, new_source)


@pytest.mark.skipif(sys.version_info < (3, 9), reason="Requires Python 3.9 or newer")
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
        ["Predictor"],
        ["predict"],
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
        return None"""
    ), "Stripped code needs to equal the minimum viable type inference."


@pytest.mark.skipif(sys.version_info < (3, 9), reason="Requires Python 3.9 or newer")
def test_strip_model_source_code_removes_function_decorators():
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
    @torch.inference_mode()
    def predict(self, msg: str) -> ModelOutput:
       return ModelOutput(success=False, error=msg, segmentedImage=None)
""",
        ["Predictor"],
        ["predict"],
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
        return None"""
    ), "Stripped code needs to equal the minimum viable type inference."


@pytest.mark.skipif(sys.version_info < (3, 9), reason="Requires Python 3.9 or newer")
def test_strip_model_source_code_keeps_referenced_globals():
    stripped_code = strip_model_source_code(
        """
import io

from cog import BasePredictor, Path
from typing import Optional
from pydantic import BaseModel
import torch
import numpy as np


INPUT_DIMS = list(np.arange(32, 64, 32))


class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]


class Predictor(BasePredictor):
    # setup code
    def predict(self, height: int=Input(description='Height of image', default=128, choices=INPUT_DIMS)) -> ModelOutput:
       return ModelOutput(success=False, error=msg, segmentedImage=None)
""",
        ["Predictor"],
        ["predict"],
    )
    assert (
        stripped_code
        == """from cog import BasePredictor, Path
from typing import Optional
from pydantic import BaseModel
import numpy as np
INPUT_DIMS = list(np.arange(32, 64, 32))
class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]
class Predictor(BasePredictor):

    def predict(self, height: int=Input(description='Height of image', default=128, choices=INPUT_DIMS)) -> ModelOutput:
        return None"""
    ), "Stripped code needs to equal the minimum viable type inference."


@pytest.mark.skipif(sys.version_info < (3, 9), reason="Requires Python 3.9 or newer")
def test_strip_model_source_code_keeps_referenced_subclasses():
    stripped_code = strip_model_source_code(
        """
import io

from cog import BasePredictor, Path
from typing import Optional
from pydantic import BaseModel
import torch
import numpy as np


INPUT_DIMS = list(np.arange(32, 64, 32))


class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]


class Predictor(BasePredictor):
    # setup code
    def predict(self, height: int=Input(description='Height of image', default=128, choices=INPUT_DIMS)) -> ModelOutput:
       return ModelOutput(success=False, error=msg, segmentedImage=None)

class SchnellPredictor(Predictor):
    # setup code
    def predict(self, height: int=Input(description='Height of image', default=128, choices=INPUT_DIMS)) -> ModelOutput:
       return ModelOutput(success=False, error=msg, segmentedImage=None)
""",
        ["SchnellPredictor"],
        ["predict"],
    )
    assert (
        stripped_code
        == """from cog import BasePredictor, Path
from typing import Optional
from pydantic import BaseModel
import numpy as np
INPUT_DIMS = list(np.arange(32, 64, 32))
class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]
class Predictor(BasePredictor):

    def predict(self, height: int=Input(description='Height of image', default=128, choices=INPUT_DIMS)) -> ModelOutput:
        return None

class SchnellPredictor(Predictor):

    def predict(self, height: int=Input(description='Height of image', default=128, choices=INPUT_DIMS)) -> ModelOutput:
        return None"""
    ), "Stripped code needs to equal the minimum viable type inference."


@pytest.mark.skipif(sys.version_info < (3, 9), reason="Requires Python 3.9 or newer")
def test_strip_model_source_code_keeps_referenced_class_from_function():
    stripped_code = strip_model_source_code(
        """
from cog import BaseModel, Input, Path

class TrainingOutput(BaseModel):
    weights: Path

def train(
    n: int,
) -> TrainingOutput:
    with open("weights.bin", "w") as fh:
        for _ in range(n):
            fh.write("a")

    return TrainingOutput(
        weights=Path("weights.bin"),
    )
""",
        ["train"],
        [],
    )
    assert (
        stripped_code
        == """from cog import BaseModel, Input, Path
class TrainingOutput(BaseModel):
    weights: Path
def train(n: int) -> TrainingOutput:
    return None"""
    ), "Stripped code needs to equal the minimum viable type inference."
