import os
import sys
from typing import Optional
from unittest.mock import patch

from cog import File, Path
from cog.predictor import get_weights_type, load_predictor_from_ref


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


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"
