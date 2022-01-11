import cog
import time


class Predictor(cog.Predictor):
    def predict(self, sleep_time: float) -> str:
        time.sleep(sleep_time)
        return "it worked!"
