"""
Configuration from cog.yaml.
"""

import os
from typing import Any, Optional

import structlog
import yaml

from .errors import ConfigDoesNotExist
from .mode import Mode

COG_YAML_FILE = "cog.yaml"
COG_PREDICT_TYPE_STUB_ENV_VAR = "COG_PREDICT_TYPE_STUB"
COG_TRAIN_TYPE_STUB_ENV_VAR = "COG_TRAIN_TYPE_STUB"
COG_MAX_CONCURRENCY_ENV_VAR = "COG_MAX_CONCURRENCY"

log = structlog.get_logger("cog.config")


class Config:
    """A class for reading the cog.yaml properties."""

    def __init__(self, config: Optional[dict[str, Any]] = None) -> None:
        self._config = config

    @property
    def _cog_config(self) -> dict[str, Any]:
        config = self._config
        if config is None:
            config_path = os.path.abspath(COG_YAML_FILE)
            try:
                with open(config_path, encoding="utf-8") as handle:
                    config = yaml.safe_load(handle)
            except FileNotFoundError as e:
                raise ConfigDoesNotExist(
                    f"Could not find {config_path}",
                ) from e
            self._config = config
        return config

    @property
    def predictor_predict_ref(self) -> Optional[str]:
        env_val = os.environ.get(COG_PREDICT_TYPE_STUB_ENV_VAR)
        if env_val:
            return env_val
        return self._cog_config.get(str(Mode.PREDICT))

    @property
    def predictor_train_ref(self) -> Optional[str]:
        env_val = os.environ.get(COG_TRAIN_TYPE_STUB_ENV_VAR)
        if env_val:
            return env_val
        return self._cog_config.get(str(Mode.TRAIN))

    @property
    def max_concurrency(self) -> int:
        env_val = os.environ.get(COG_MAX_CONCURRENCY_ENV_VAR)
        if env_val:
            return int(env_val)
        return int(self._cog_config.get("concurrency", {}).get("max", 1))

    def get_predictor_ref(self, mode: Mode) -> str:
        predictor_ref = None
        if mode == Mode.PREDICT:
            predictor_ref = self.predictor_predict_ref
        elif mode == Mode.TRAIN:
            predictor_ref = self.predictor_train_ref
        if predictor_ref is None:
            raise ValueError(
                f"Can't run predictions: '{mode}' option not found in cog.yaml"
            )
        return predictor_ref
