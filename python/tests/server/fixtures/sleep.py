import time

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, sleep: float = 0) -> str:
        print("starting")
        time.sleep(sleep)
        return f"done in {sleep} seconds"
