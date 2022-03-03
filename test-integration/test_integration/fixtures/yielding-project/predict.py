from typing import Iterator

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, text: str) -> Iterator[str]:
        predictions = ["foo", "bar", "baz"]
        for prediction in predictions:
            yield prediction
