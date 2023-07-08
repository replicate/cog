from typing import Optional

from cog import File, Path
from cog.predictor import get_weights_type


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
