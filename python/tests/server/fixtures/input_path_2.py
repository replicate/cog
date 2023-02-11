from cog import BasePredictor, Path


class Predictor(BasePredictor):
    def predict(self, path: Path) -> str:
        return str(path)
