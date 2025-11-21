from cog import BasePredictor, Image, Input


class Predictor(BasePredictor):
    def predict(
        self,
        image: Image = Input(description="An input image"),
    ) -> str:
        return "success"
