import cog
import time


class Predictor(cog.Predictor):
    def setup(self):
        pass

    @cog.input("sleep_time", type=float)
    def predict(self, sleep_time):
        time.sleep(sleep_time)
        return "it worked!"
