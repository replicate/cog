import os
import tempfile

import pytest

from cog.config import (
    COG_GPU_ENV_VAR,
    COG_PREDICTOR_PREDICT_ENV_VAR,
    COG_PREDICTOR_TRAIN_ENV_VAR,
    COG_YAML_FILE,
    Config,
)
from cog.errors import ConfigDoesNotExist


def test_predictor_predict_ref_env_var():
    predict_ref = "predict.py:Predictor"
    os.environ[COG_PREDICTOR_PREDICT_ENV_VAR] = predict_ref
    config = Config()
    config_predict_ref = config.predictor_predict_ref
    del os.environ[COG_PREDICTOR_PREDICT_ENV_VAR]
    assert (
        config_predict_ref == predict_ref
    ), "Predict Reference should come from the environment variable."


def test_predictor_predict_ref_no_env_var():
    if COG_PREDICTOR_PREDICT_ENV_VAR in os.environ:
        del os.environ[COG_PREDICTOR_PREDICT_ENV_VAR]
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
        assert (
            config_predict_ref == "predict.py:Predictor"
        ), "Predict Reference should come from the cog config file."
    os.chdir(pwd)


def test_config_no_config_file():
    if COG_PREDICTOR_PREDICT_ENV_VAR in os.environ:
        del os.environ[COG_PREDICTOR_PREDICT_ENV_VAR]
    config = Config()
    with pytest.raises(ConfigDoesNotExist):
        _ = config.predictor_predict_ref


def test_config_initial_values():
    if COG_PREDICTOR_PREDICT_ENV_VAR in os.environ:
        del os.environ[COG_PREDICTOR_PREDICT_ENV_VAR]
    config = Config(config={"predict": "predict.py:Predictor"})
    config_predict_ref = config.predictor_predict_ref
    assert (
        config_predict_ref == "predict.py:Predictor"
    ), "Predict Reference should come from the initial config dictionary."


def test_predictor_train_ref_env_var():
    train_ref = "predict.py:Predictor"
    os.environ[COG_PREDICTOR_TRAIN_ENV_VAR] = train_ref
    config = Config()
    config_train_ref = config.predictor_train_ref
    del os.environ[COG_PREDICTOR_TRAIN_ENV_VAR]
    assert (
        config_train_ref == train_ref
    ), "Train Reference should come from the environment variable."


def test_predictor_train_ref_no_env_var():
    train_ref = "predict.py:Predictor"
    if COG_PREDICTOR_TRAIN_ENV_VAR in os.environ:
        del os.environ[COG_PREDICTOR_TRAIN_ENV_VAR]
    config = Config(config={"train": train_ref})
    config_train_ref = config.predictor_train_ref
    assert (
        config_train_ref == train_ref
    ), "Train Reference should come from the initial config dictionary."


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
    assert (
        not config_gpu
    ), "Requires GPU should come from the initial config dictionary."
