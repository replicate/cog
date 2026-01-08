from typing import Optional, Union

from cog import BasePredictor, Path

FIXTURE = [({'i': 0}, '')]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self, weights: Optional[Union[Path, str]]) -> None:
        self.setup_done = True
        self.weights = weights

    def predict(self, i: int) -> str:
        return '' if self.weights is None else str(self.weights)
