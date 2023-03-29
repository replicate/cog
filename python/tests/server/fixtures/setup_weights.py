from cog import File


class Predictor:
    def setup(self, weights: File):
        self.text = weights.read()

    def predict(self) -> None:
        return self.text
