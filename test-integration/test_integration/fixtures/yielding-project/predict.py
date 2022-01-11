from typing import Generator
import cog


class Predictor(cog.Predictor):
    def predict(self, text: str) -> Generator[str, None, None]:
        predictions = ["foo", "bar", "baz"]
        for prediction in predictions:
            yield prediction
