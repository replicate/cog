from cog import BasePredictor

FIXTURE = [
    ({'x': 1, 'y': 2}, 3),
    ({'x': 2, 'y': 3}, 5),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, x: int, y: int) -> int:
        return x + y
