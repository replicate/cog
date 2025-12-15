from typing import Optional

from cog import BasePredictor
from tests.util import check_python_version

check_python_version(min_version=(3, 10))

FIXTURE = [
    (
        {
            'os1': 'foo0',
            'os2': 'bar0',
            'os3': 'baz0',
        },
        'foo0-bar0-baz0',
    ),
]


class Predictor(BasePredictor):
    test_inputs = {
        'os1': 'foo',
        'os2': 'bar',
        'os3': 'baz',
    }
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        os1: Optional[str],
        os2: str | None,
        os3: None | str,
    ) -> str:
        return f'{os1}-{os2}-{os3}'
