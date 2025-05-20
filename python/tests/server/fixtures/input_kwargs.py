from typing import Dict

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, **kwargs) -> Dict:
        return kwargs
