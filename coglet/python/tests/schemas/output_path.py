from cog import BasePredictor, Path

FIXTURE = [
    ({'x': 'foo.txt'}, Path('/tmp/foo.txt')),
    ({'x': Path('bar.txt')}, Path('/tmp/bar.txt')),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, x: Path) -> Path:
        return Path('/tmp').joinpath(x)
