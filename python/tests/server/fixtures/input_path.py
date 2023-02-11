from cog import BasePredictor, Path


class Predictor(BasePredictor):
    def predict(self, path: Path) -> str:
        with open(path) as fh:
            extension = fh.name.split(".")[-1]
            return f"{extension} {fh.read()}"
