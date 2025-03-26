from cog import BasePredictor, Path


class Predictor(BasePredictor):
    def predict(self, file: Path | None) -> str:
        print(f"file: {file}")
        return "hello"
