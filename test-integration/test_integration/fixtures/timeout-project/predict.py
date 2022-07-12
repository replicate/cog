import time

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, sleep_time: float) -> str:
        time.sleep(sleep_time)
        return f"it worked after {sleep_time} seconds!"
