from typing import Generator

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, text: str) -> Generator[str, None, None]:
        predictions = ["foo", "bar", "baz"]
        for prediction in predictions:
            yield prediction
