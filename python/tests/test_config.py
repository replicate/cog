import os
import tempfile

from cog.config import (
    Config,
)
from cog.mode import Mode


def test_get_predictor_ref_predict():
    train_ref = "predict.py:Predictor"
    config = Config(config={"train": train_ref})
    config_train_ref = config.get_predictor_ref(Mode.TRAIN)
    assert (
        train_ref == config_train_ref
    ), "The train ref should equal the config train ref."


def test_get_predictor_ref_train():
    predict_ref = "predict.py:Predictor"
    config = Config(config={"predict": predict_ref})
    config_predict_ref = config.get_predictor_ref(Mode.PREDICT)
    assert (
        predict_ref == config_predict_ref
    ), "The predict ref should equal the config predict ref."


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
        input_type, output_type = config.get_predictor_types(Mode.PREDICT)
        assert (
            str(input_type) == "<class 'cog.predictor.Input'>"
        ), "Predict input type should be the predictor Input."
        assert (
            str(output_type)
            == "<class 'cog.predictor.get_output_type.<locals>.Output'>"
        ), "Predict output type should be the predictor Output."


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
        input_type, output_type = config.get_predictor_types(Mode.TRAIN)
        assert (
            str(input_type) == "<class 'cog.predictor.TrainingInput'>"
        ), "Predict input type should be the training Input."
        assert str(output_type).endswith(
            "TrainingOutput'>"
        ), "Predict output type should be the training Output."
