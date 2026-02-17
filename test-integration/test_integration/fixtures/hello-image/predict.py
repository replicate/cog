from cog import BasePredictor, Path


class Predictor(BasePredictor):
    def predict(self, word: str) -> Path:
        return Path("hello.webp")
