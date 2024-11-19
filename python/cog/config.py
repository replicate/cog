import inspect
import os
import sys
import uuid
from typing import Any, Callable, Optional, Tuple, Type

import structlog
import yaml
from pydantic import BaseModel

from .base_input import BaseInput
from .base_predictor import BasePredictor
from .code_xforms import load_module_from_string, strip_model_source_code
from .env_property import env_property
from .errors import ConfigDoesNotExist
from .mode import Mode
from .predictor import (
    get_input_type,
    get_output_type,
    get_predict,
    get_predictor,
    get_train,
    get_training_input_type,
    get_training_output_type,
    load_full_predictor_from_file,
)
from .types import CogConfig
from .wait import wait_for_env

COG_YAML_FILE = "cog.yaml"
COG_PREDICT_TYPE_STUB_ENV_VAR = "COG_PREDICT_TYPE_STUB"
COG_TRAIN_TYPE_STUB_ENV_VAR = "COG_TRAIN_TYPE_STUB"
COG_PREDICT_CODE_STRIP_ENV_VAR = "COG_PREDICT_CODE_STRIP"
COG_TRAIN_CODE_STRIP_ENV_VAR = "COG_TRAIN_CODE_STRIP"
COG_GPU_ENV_VAR = "COG_GPU"
COG_MAX_CONCURRENCY_ENV_VAR = "COG_MAX_CONCURRENCY"
PREDICT_METHOD_NAME = "predict"
TRAIN_METHOD_NAME = "train"

log = structlog.get_logger("cog.config")


def _method_name_from_mode(mode: Mode) -> str:
    if mode == Mode.PREDICT:
        return PREDICT_METHOD_NAME
    elif mode == Mode.TRAIN:
        return TRAIN_METHOD_NAME
    raise ValueError(f"Mode {mode} not recognised for method name mapping")


def _env_var_from_mode(mode: Mode) -> str:
    if mode == Mode.PREDICT:
        return COG_PREDICT_CODE_STRIP_ENV_VAR
    elif mode == Mode.TRAIN:
        return COG_TRAIN_CODE_STRIP_ENV_VAR
    raise ValueError(f"Mode {mode} not recognised for env var mapping")


class Config:
    """A class for reading the cog.yaml properties."""

    def __init__(self, config: Optional[CogConfig] = None) -> None:
        self._config = config

    @property
    def _cog_config(self) -> CogConfig:
        """
        Warning: Do not access this directly outside this class, instead
        write an explicit public property and back it by an @env_property
        to allow for the possibility of injecting the property you are
        trying to read without relying on the underlying file.
        """
        config = self._config
        if config is None:
            wait_for_env(include_imports=False)
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
    @env_property(COG_PREDICT_TYPE_STUB_ENV_VAR)
    def predictor_predict_ref(self) -> Optional[str]:
        """Find the predictor ref for the predict mode."""
        return self._cog_config.get(str(Mode.PREDICT))

    @property
    @env_property(COG_TRAIN_TYPE_STUB_ENV_VAR)
    def predictor_train_ref(self) -> Optional[str]:
        """Find the predictor ref for the train mode."""
        return self._cog_config.get(str(Mode.TRAIN))

    @property
    @env_property(COG_GPU_ENV_VAR)
    def requires_gpu(self) -> bool:
        """Whether this cog requires the use of a GPU."""
        return bool(self._cog_config.get("build", {}).get("gpu", False))

    @property
    @env_property(COG_MAX_CONCURRENCY_ENV_VAR)
    def max_concurrency(self) -> int:
        """The maximum concurrency of predictions supported by this model. Defaults to 1."""
        return int(self._cog_config.get("concurrency", {}).get("max", 1))

    def _predictor_code(
        self,
        module_path: str,
        class_name: str,
        method_name: str,
        mode: Mode,
        module_name: str,
    ) -> Optional[str]:
        source_code = os.environ.get(_env_var_from_mode(mode))
        if source_code is not None:
            return source_code
        if sys.version_info >= (3, 9):
            wait_for_env(include_imports=False)
            with open(module_path, encoding="utf-8") as file:
                return strip_model_source_code(file.read(), [class_name], [method_name])
        else:
            log.debug(f"[{module_name}] cannot use fast loader as current Python <3.9")
        return None

    def _load_predictor_for_types(
        self, ref: str, method_name: str, mode: Mode
    ) -> BasePredictor:
        module_path, class_name = ref.split(":", 1)
        module_name = os.path.basename(module_path).split(".py", 1)[0]
        code = self._predictor_code(
            module_path, class_name, method_name, mode, module_name
        )
        module = None
        if code is not None:
            try:
                module = load_module_from_string(uuid.uuid4().hex, code)
            except Exception as e:  # pylint: disable=broad-exception-caught
                log.info(f"[{module_name}] fast loader failed: {e}")
        if module is None:
            log.debug(f"[{module_name}] falling back to slow loader")
            wait_for_env(include_imports=False)
            module = load_full_predictor_from_file(module_path, module_name)
        return get_predictor(module, class_name)

    def get_predictor_ref(self, mode: Mode) -> str:
        """Find the predictor reference for a given mode."""
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

    def get_predictor_types(
        self, mode: Mode
    ) -> Tuple[Type[BaseInput], Type[BaseModel], bool]:
        """
        Find the input & output types of a predictor/train function as well
        as determining if the function is an async function.
        """
        predictor_ref = self.get_predictor_ref(mode=mode)
        predictor = self._load_predictor_for_types(
            predictor_ref, _method_name_from_mode(mode=mode), mode
        )

        def is_async(fn: Callable[[Any], Any]) -> bool:
            return inspect.iscoroutinefunction(fn) or inspect.isasyncgenfunction(fn)

        if mode == Mode.PREDICT:
            return (
                get_input_type(predictor),
                get_output_type(predictor),
                is_async(get_predict(predictor)),
            )
        elif mode == Mode.TRAIN:
            return (
                get_training_input_type(predictor),
                get_training_output_type(predictor),
                is_async(get_train(predictor)),
            )
        raise ValueError(f"Mode {mode} not found for generating input/output types.")
