from cog import BasePredictor

import tensorflow


class Predictor(BasePredictor):
    def predict(self) -> str:
        return tensorflow.__version__
