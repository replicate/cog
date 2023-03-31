import time

from cog import BasePredictor


class Predictor(BasePredictor):
    def setup(self):
        time.sleep(4)

    def predict(self, sleep: float = 0) -> str:
        time.sleep(sleep)
        return f"done in {sleep} seconds"
