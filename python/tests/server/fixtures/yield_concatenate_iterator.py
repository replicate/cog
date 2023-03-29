from cog import BasePredictor, ConcatenateIterator


class Predictor(BasePredictor):
    def predict(
        self,
    ) -> ConcatenateIterator[str]:
        predictions = ["foo", "bar", "baz"]
        for prediction in predictions:
            yield prediction
