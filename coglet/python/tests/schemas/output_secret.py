from cog import BasePredictor, Secret

FIXTURE = [
    ({'x': 'foo'}, Secret('foo')),
    ({'x': Secret('bar')}, Secret('bar')),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, x: Secret) -> Secret:
        return x
