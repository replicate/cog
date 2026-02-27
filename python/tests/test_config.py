"""Tests for cog.config.Config class."""

import os

import pytest

from cog.config import Config
from cog.mode import Mode


class TestConfigRunKey:
    """Test that Config supports the 'run' key in cog.yaml."""

    def test_run_key(self) -> None:
        config = Config({"run": "run.py:Runner"})
        assert config.predictor_predict_ref == "run.py:Runner"

    def test_predict_key(self) -> None:
        config = Config({"predict": "predict.py:Predictor"})
        assert config.predictor_predict_ref == "predict.py:Predictor"

    def test_run_key_preferred_over_predict(self) -> None:
        """If both 'run' and 'predict' are set, raise an error."""
        config = Config({"run": "run.py:Runner", "predict": "predict.py:Predictor"})
        with pytest.raises(ValueError, match="both 'run' and 'predict'"):
            _ = config.predictor_predict_ref

    def test_neither_run_nor_predict(self) -> None:
        config = Config({"build": {"gpu": False}})
        assert config.predictor_predict_ref is None

    def test_get_predictor_ref_run_key(self) -> None:
        config = Config({"run": "run.py:Runner"})
        assert config.get_predictor_ref(Mode.PREDICT) == "run.py:Runner"

    def test_get_predictor_ref_predict_key(self) -> None:
        config = Config({"predict": "predict.py:Predictor"})
        assert config.get_predictor_ref(Mode.PREDICT) == "predict.py:Predictor"

    def test_get_predictor_ref_missing(self) -> None:
        config = Config({"build": {}})
        with pytest.raises(ValueError, match="'run' option not found"):
            config.get_predictor_ref(Mode.PREDICT)

    def test_get_predictor_ref_train(self) -> None:
        config = Config({"train": "train.py:Train"})
        assert config.get_predictor_ref(Mode.TRAIN) == "train.py:Train"

    def test_get_predictor_ref_train_missing(self) -> None:
        config = Config({"run": "run.py:Runner"})
        with pytest.raises(ValueError, match="'train' option not found"):
            config.get_predictor_ref(Mode.TRAIN)

    def test_env_var_overrides_run_key(self) -> None:
        config = Config({"run": "run.py:Runner"})
        os.environ["COG_PREDICT_TYPE_STUB"] = "override.py:Override"
        try:
            assert config.predictor_predict_ref == "override.py:Override"
        finally:
            del os.environ["COG_PREDICT_TYPE_STUB"]
