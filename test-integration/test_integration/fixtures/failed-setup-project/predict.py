from time import sleep

from cog import BasePredictor


class Predictor(BasePredictor):
    def setup(self):
        raise Exception("Failed during setup")

    def predict(self) -> str:
        return "done"
