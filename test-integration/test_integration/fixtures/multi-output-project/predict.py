import cog
import time


class Predictor(cog.Predictor):
    def setup(self):
        pass

    def predict(self):
        return {
            "output1": "it worked!",
            "output2": [1, 2, 3, 4],
        }
