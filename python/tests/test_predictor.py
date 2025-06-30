import inspect
import os
import sys
from typing import Optional
from unittest.mock import patch

from pydantic.fields import FieldInfo

from cog import File, Input, Path
from cog.predictor import (
    get_input_create_model_kwargs,
    get_predict,
    get_weights_type,
    load_predictor_from_ref,
)
from cog.types import PYDANTIC_V2

if PYDANTIC_V2:
    from pydantic.fields import PydanticUndefined
else:
    from pydantic.fields import Undefined as PydanticUndefined


def is_field_required(field: FieldInfo):
    if hasattr(field, "is_required"):
        return field.is_required()
    if hasattr(field, "required"):
        return field.required
    return field.default is PydanticUndefined and field.default_factory is None


def test_get_weights_type() -> None:
    def f() -> None:
        pass

    assert get_weights_type(f) is None

    def f(weights: File) -> None:
        pass

    assert get_weights_type(f) == File

    def f(weights: Path) -> None:
        pass

    assert get_weights_type(f) == Path

    def f(weights: Optional[File]) -> None:
        pass

    assert get_weights_type(f) == File


def test_load_predictor_from_ref_overrides_argv():
    with patch("sys.argv", ["foo.py", "exec", "--giraffes=2", "--eat-cookies"]):
        predictor = load_predictor_from_ref(_fixture_path("argv_override"))

        # check the predictor module saw no args
        assert predictor.predict() == ["foo.py"]
        # check we reset the args correctly
        assert sys.argv == ["foo.py", "exec", "--giraffes=2", "--eat-cookies"]


def test_get_input_create_model_kwargs():
    def predict(thing: Optional[str] = Input(description="Hello String.")) -> str:
        return thing if thing is not None else "Nothing"

    predict_type = get_predict(predict)
    signature = inspect.signature(predict_type)
    output = get_input_create_model_kwargs(signature)
    assert not is_field_required(output["thing"][1])


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"
