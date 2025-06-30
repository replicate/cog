from cog import BasePredictor

import numpy as np


class Predictor(BasePredictor):

    def predict(self) -> str:
        return "hello " + np.__version__
