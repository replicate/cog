from cog import BasePredictor


class Predictor(BasePredictor):
    def setup(self):
        raise Exception("setup failed")

    def predict(self) -> str:
        return "nope"
