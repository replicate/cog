import io

from cog import BasePredictor, File


class Predictor(BasePredictor):
    def predict(self) -> File:
        return io.StringIO("hello")
