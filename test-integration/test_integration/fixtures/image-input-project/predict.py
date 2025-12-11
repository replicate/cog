from cog import BasePredictor, Image


class Predictor(BasePredictor):
    def predict(self, image: Image) -> str:
        with open(image) as f:
            content = f.read()
        return f"image: {content}"
