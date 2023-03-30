from cog import BasePredictor, File


class Predictor(BasePredictor):
    def setup(self, weights: File):
        self.text = weights.read()

    def predict(self) -> str:
        return f"hello {self.text}"
