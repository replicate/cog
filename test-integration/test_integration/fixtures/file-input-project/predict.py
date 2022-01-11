import cog
from cog import Path


class Predictor(cog.Predictor):
    def predict(self, path: Path) -> str:
        with open(path) as f:
            return f.read()
