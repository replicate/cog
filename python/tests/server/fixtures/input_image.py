from cog import BasePredictor, Image


class Predictor(BasePredictor):
    def predict(self, image: Image) -> str:
        with open(image) as fh:
            extension = fh.name.split(".")[-1]
            return f"{extension} {fh.read()}"
