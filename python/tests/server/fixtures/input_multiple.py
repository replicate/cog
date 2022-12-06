from cog import BasePredictor, Input, Path


class Predictor(BasePredictor):
    def predict(
        self,
        text: str,
        path: Path,
        num1: int,
        num2: int = Input(default=10),
    ) -> str:
        with open(path) as fh:
            return text + " " + str(num1 * num2) + " " + fh.read()
