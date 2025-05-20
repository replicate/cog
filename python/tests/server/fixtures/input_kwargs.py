from typing import Union, Dict

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, **kwargs: Union[str, int]) -> Dict:
        return kwargs
