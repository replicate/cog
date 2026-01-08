from cog import BasePredictor

FIXTURE = [
    ({'x': 1.1, 'y': 2.2}, '3.30'),
    ({'x': 2.2, 'y': 3.3}, '5.50'),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, x: float, y: float) -> str:
        return f'{x + y:.2f}'
