import json

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, **kwargs: str | int) -> str:
        return json.dumps(kwargs)
