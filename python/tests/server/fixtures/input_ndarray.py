from cog import BasePredictor
import numpy as np


class Predictor(BasePredictor):
    def predict(self, array: np.ndarray):
        assert array == np.array([[1, 2], [3, 4]])
