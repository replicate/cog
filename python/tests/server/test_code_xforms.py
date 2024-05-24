import os
import sys
import uuid

import cog.code_xforms as code_xforms
import pytest

g_module_dir = os.path.dirname(os.path.abspath(__file__))


@pytest.mark.skipif(sys.version_info < (3, 9), reason="requires python3.9 or higher")
def test_train_function_model():
    with open(f"{g_module_dir}/fixtures/train.py", encoding="utf-8") as file:
        source_code = file.read()

    new_source = code_xforms.strip_model_source_code(source_code, "train", "train")
    expected_source = """
from cog import BaseModel, Input, Path

class TrainingOutput(BaseModel):
    weights: Path

def train(n: int=Input(description='Dimension of weights to generate')) -> TrainingOutput:
    return None
"""
    assert expected_source.strip() == new_source.strip()
    assert code_xforms.load_module_from_string(uuid.uuid4().hex, new_source)


@pytest.mark.skipif(sys.version_info < (3, 9), reason="requires python3.9 or higher")
def test_predict_many_inputs():
    with open(
        f"{g_module_dir}/../../../test-integration/test_integration/fixtures/many-inputs-project/predict.py",
        encoding="utf-8",
    ) as file:
        source_code = file.read()

    new_source = code_xforms.strip_model_source_code(
        source_code, "Predictor", "predict"
    )
    expected_source = """
from cog import BasePredictor, Input, Path



class Predictor(BasePredictor):

    def predict(self, no_default: str, default_without_input: str='default', input_with_default: int=Input(default=10), path: Path=Input(description='Some path'), image: Path=Input(description='Some path'), choices: str=Input(choices=['foo', 'bar']), int_choices: int=Input(description='hello', choices=[3, 4, 5])) -> str:
        return None
"""
    assert expected_source.strip() == new_source.strip()
    assert code_xforms.load_module_from_string(uuid.uuid4().hex, new_source)


@pytest.mark.skipif(sys.version_info < (3, 9), reason="requires python3.9 or higher")
def test_predict_output_path_model():
    with open(
        f"{g_module_dir}/../../../test-integration/test_integration/fixtures/path-output-project/predict.py",
        encoding="utf-8",
    ) as file:
        source_code = file.read()

    new_source = code_xforms.strip_model_source_code(
        source_code, "Predictor", "predict"
    )
    expected_source = """
import os
from cog import BasePredictor, Path



class Predictor(BasePredictor):

    def predict(self) -> Path:
        return None
"""
    assert expected_source.strip() == new_source.strip()
    assert code_xforms.load_module_from_string(uuid.uuid4().hex, new_source)
