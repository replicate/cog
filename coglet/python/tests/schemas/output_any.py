from typing import Any

from cog import BasePredictor

FIXTURE = [
    ({'x': 1}, 'foo'),
    ({'x': 2}, {'msg': 'bar'}),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, x: int) -> Any:
        if x == 1:
            return 'foo'
        elif x == 2:
            return {'msg': 'bar'}
