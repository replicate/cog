from typing import Optional

from cog import BasePredictor, Input
from tests.util import check_python_version

check_python_version(min_version=(3, 10))

ERROR = 'error-prone usage of default=None'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self,
        i1a: int,
        i1b: Optional[int],
        i1c: int | None,
        i2a: int = Input(),
        i2b: int = Input(default=None),  # Bad
        i2c: int = Input(default=1),
        i3a: Optional[int] = Input(),
        i3b: Optional[int] = Input(default=None),
        i3c: Optional[int] = Input(default=1),
        i4a: int | None = Input(),
        i4b: int | None = Input(default=None),
        i4c: int | None = Input(default=1),
    ) -> str:
        return 'foo'
