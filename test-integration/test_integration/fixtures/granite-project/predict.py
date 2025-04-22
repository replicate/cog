from cog import BasePredictor

import pydantic


class Predictor(BasePredictor):
    def predict(self) -> str:
        return pydantic.__version__
