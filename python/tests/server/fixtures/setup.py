from cog.predictor import BasePredictor


class Predictor(BasePredictor):
    def setup(self):
        self.foo = "bar"

    def predict(self) -> str:
        return self.foo
