from cog import BasePredictor

import os


class Predictor(BasePredictor):
    def predict(self) -> str:
        return "hello " + ",".join(os.listdir("/bin"))
