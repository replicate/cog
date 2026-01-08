from cog import BasePredictor

FIXTURE = [
    ({'x': False, 'y': False}, False),
    ({'x': False, 'y': True}, True),
    ({'x': True, 'y': False}, True),
    ({'x': True, 'y': True}, True),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, x: bool, y: bool) -> bool:
        return x or y
