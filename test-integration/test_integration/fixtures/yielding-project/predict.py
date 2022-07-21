from time import sleep
from typing import Iterator

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, text: str) -> Iterator[str]:
        sleep(0.2)  # sleep to help test timing

        predictions = ["foo", "bar", "baz"]
        for prediction in predictions:
            yield prediction

        sleep(0.2)  # sleep to help test timing
