from cog import BasePredictor

FIXTURE = [
    ({'x': 'foo'}, '*foo*'),
    ({'x': 'bar'}, '*bar*'),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, x: str) -> str:
        return f'*{x}*'
