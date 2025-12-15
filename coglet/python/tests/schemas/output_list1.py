from typing import List

from cog import BasePredictor

FIXTURE = [
    ({'xs': []}, []),
    ({'xs': [0, 1, 2]}, [1, 2, 3]),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, xs: List[int]) -> List[int]:
        return [x + 1 for x in xs]
