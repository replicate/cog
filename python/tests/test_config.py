import os
import tempfile

import pytest

from cog.config import (
    COG_GPU_ENV_VAR,
    COG_PREDICT_CODE_STRIP_ENV_VAR,
    COG_PREDICT_TYPE_STUB_ENV_VAR,
    COG_TRAIN_TYPE_STUB_ENV_VAR,
    COG_YAML_FILE,
    Config,
)
from cog.errors import ConfigDoesNotExist
from cog.mode import Mode


def test_predictor_predict_ref_env_var():
    predict_ref = "predict.py:Predictor"
    os.environ[COG_PREDICT_TYPE_STUB_ENV_VAR] = predict_ref
    config = Config()
    config_predict_ref = config.predictor_predict_ref
    del os.environ[COG_PREDICT_TYPE_STUB_ENV_VAR]
    assert config_predict_ref == predict_ref, (
        "Predict Reference should come from the environment variable."
    )


def test_predictor_predict_ref_no_env_var():
    if COG_PREDICT_TYPE_STUB_ENV_VAR in os.environ:
        del os.environ[COG_PREDICT_TYPE_STUB_ENV_VAR]
    pwd = os.getcwd()
    with tempfile.TemporaryDirectory() as tmpdir:
        os.chdir(tmpdir)
        with open(COG_YAML_FILE, "w", encoding="utf-8") as handle:
            handle.write("""
build:
  python_version: "3.11"
predict: "predict.py:Predictor"
""")
        config = Config()
        config_predict_ref = config.predictor_predict_ref
        assert config_predict_ref == "predict.py:Predictor", (
            "Predict Reference should come from the cog config file."
        )
    os.chdir(pwd)


def test_config_no_config_file():
    if COG_PREDICT_TYPE_STUB_ENV_VAR in os.environ:
        del os.environ[COG_PREDICT_TYPE_STUB_ENV_VAR]
    config = Config()
    with pytest.raises(ConfigDoesNotExist):
        _ = config.predictor_predict_ref


def test_config_initial_values():
    if COG_PREDICT_TYPE_STUB_ENV_VAR in os.environ:
        del os.environ[COG_PREDICT_TYPE_STUB_ENV_VAR]
    config = Config(config={"predict": "predict.py:Predictor"})
    config_predict_ref = config.predictor_predict_ref
    assert config_predict_ref == "predict.py:Predictor", (
        "Predict Reference should come from the initial config dictionary."
    )


def test_predictor_train_ref_env_var():
    train_ref = "predict.py:Predictor"
    os.environ[COG_TRAIN_TYPE_STUB_ENV_VAR] = train_ref
    config = Config()
    config_train_ref = config.predictor_train_ref
    del os.environ[COG_TRAIN_TYPE_STUB_ENV_VAR]
    assert config_train_ref == train_ref, (
        "Train Reference should come from the environment variable."
    )


def test_predictor_train_ref_no_env_var():
    train_ref = "predict.py:Predictor"
    if COG_TRAIN_TYPE_STUB_ENV_VAR in os.environ:
        del os.environ[COG_TRAIN_TYPE_STUB_ENV_VAR]
    config = Config(config={"train": train_ref})
    config_train_ref = config.predictor_train_ref
    assert config_train_ref == train_ref, (
        "Train Reference should come from the initial config dictionary."
    )


def test_requires_gpu_env_var():
    gpu = True
    os.environ[COG_GPU_ENV_VAR] = str(gpu)
    config = Config()
    config_gpu = config.requires_gpu
    del os.environ[COG_GPU_ENV_VAR]
    assert config_gpu, "Requires GPU should come from the environment variable."


def test_requires_gpu_no_env_var():
    if COG_GPU_ENV_VAR in os.environ:
        del os.environ[COG_GPU_ENV_VAR]
    config = Config(config={"build": {"gpu": False}})
    config_gpu = config.requires_gpu
    assert not config_gpu, (
        "Requires GPU should come from the initial config dictionary."
    )


def test_get_predictor_ref_predict():
    train_ref = "predict.py:Predictor"
    config = Config(config={"train": train_ref})
    config_train_ref = config.get_predictor_ref(Mode.TRAIN)
    assert train_ref == config_train_ref, (
        "The train ref should equal the config train ref."
    )


def test_get_predictor_ref_train():
    predict_ref = "predict.py:Predictor"
    config = Config(config={"predict": predict_ref})
    config_predict_ref = config.get_predictor_ref(Mode.PREDICT)
    assert predict_ref == config_predict_ref, (
        "The predict ref should equal the config predict ref."
    )


def test_get_predictor_types_with_env_var():
    predict_ref = "predict.py:Predictor"
    os.environ[COG_PREDICT_TYPE_STUB_ENV_VAR] = predict_ref
    os.environ[COG_PREDICT_CODE_STRIP_ENV_VAR] = """
from cog import BasePredictor, Path
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
    config = Config()
    input_type, output_type, is_async = config.get_predictor_types(Mode.PREDICT)
    del os.environ[COG_PREDICT_CODE_STRIP_ENV_VAR]
    del os.environ[COG_PREDICT_TYPE_STUB_ENV_VAR]
    assert str(input_type) == "<class 'cog.predictor.Input'>", (
        "Predict input type should be the predictor Input."
    )
    assert (
        str(output_type) == "<class 'cog.predictor.get_output_type.<locals>.Output'>"
    ), "Predict output type should be the predictor Output."
    assert not is_async, "is_async should be False for normal functions"


def test_get_predictor_types():
    with tempfile.TemporaryDirectory() as tmpdir:
        predict_python_file = os.path.join(tmpdir, "predict.py")
        with open(predict_python_file, "w", encoding="utf-8") as handle:
            handle.write("""
import io

from cog import BasePredictor, Path
from typing import Optional
from pydantic import BaseModel


class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]


class Predictor(BasePredictor):
    # setup code
    def predict(self, msg: str) -> ModelOutput:
       return ModelOutput(success=False, error=msg, segmentedImage=None)
""")
        predict_ref = f"{predict_python_file}:Predictor"
        config = Config(config={"predict": predict_ref})
        input_type, output_type, is_async = config.get_predictor_types(Mode.PREDICT)
        assert str(input_type) == "<class 'cog.predictor.Input'>", (
            "Predict input type should be the predictor Input."
        )
        assert (
            str(output_type)
            == "<class 'cog.predictor.get_output_type.<locals>.Output'>"
        ), "Predict output type should be the predictor Output."
        assert not is_async, "is_async should be False for normal functions"


def test_get_predictor_types_with_async():
    with tempfile.TemporaryDirectory() as tmpdir:
        predict_python_file = os.path.join(tmpdir, "predict.py")
        with open(predict_python_file, "w", encoding="utf-8") as handle:
            handle.write("""
import io

from cog import BasePredictor, Path
from typing import Optional
from pydantic import BaseModel


class ModelOutput(BaseModel):
    success: bool
    error: Optional[str]
    segmentedImage: Optional[Path]


class Predictor(BasePredictor):
    # setup code
    async def predict(self, msg: str) -> ModelOutput:
       return ModelOutput(success=False, error=msg, segmentedImage=None)
""")
        predict_ref = f"{predict_python_file}:Predictor"
        config = Config(config={"predict": predict_ref})
        input_type, output_type, is_async = config.get_predictor_types(Mode.PREDICT)
        assert str(input_type) == "<class 'cog.predictor.Input'>", (
            "Predict input type should be the predictor Input."
        )
        assert (
            str(output_type)
            == "<class 'cog.predictor.get_output_type.<locals>.Output'>"
        ), "Predict output type should be the predictor Output."
        assert is_async, "is_async should be True for async functions"


def test_get_predictor_types_for_train():
    with tempfile.TemporaryDirectory() as tmpdir:
        predict_python_file = os.path.join(tmpdir, "train.py")
        with open(predict_python_file, "w", encoding="utf-8") as handle:
            handle.write("""
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
""")
        train_ref = f"{predict_python_file}:train"
        config = Config(config={"train": train_ref})
        input_type, output_type, is_async = config.get_predictor_types(Mode.TRAIN)
        assert str(input_type) == "<class 'cog.predictor.TrainingInput'>", (
            "Predict input type should be the training Input."
        )
        assert str(output_type).endswith("TrainingOutput'>"), (
            "Predict output type should be the training Output."
        )
        assert not is_async, "is_async should be False for normal functions"


def test_get_predictor_types_for_train_with_async():
    with tempfile.TemporaryDirectory() as tmpdir:
        predict_python_file = os.path.join(tmpdir, "train.py")
        with open(predict_python_file, "w", encoding="utf-8") as handle:
            handle.write("""
from cog import BaseModel, Input, Path

class TrainingOutput(BaseModel):
    weights: Path

async def train(
    n: int,
) -> TrainingOutput:
    with open("weights.bin", "w") as fh:
        for _ in range(n):
            fh.write("a")

    return TrainingOutput(
        weights=Path("weights.bin"),
    )
""")
        train_ref = f"{predict_python_file}:train"
        config = Config(config={"train": train_ref})
        input_type, output_type, is_async = config.get_predictor_types(Mode.TRAIN)
        assert str(input_type) == "<class 'cog.predictor.TrainingInput'>", (
            "Predict input type should be the training Input."
        )
        assert str(output_type).endswith("TrainingOutput'>"), (
            "Predict output type should be the training Output."
        )
        assert is_async, "is_async should be True for async functions"
