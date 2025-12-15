from typing import List

from cog import BasePredictor, Input
from tests.util import check_python_version

check_python_version(min_version=(3, 10))

ERROR = 'error-prone usage of default=None'


class Predictor(BasePredictor):
    def predict(self, x: List[str] = Input(default=None)) -> str:
        pass
