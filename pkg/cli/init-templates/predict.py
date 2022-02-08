# Prediction interface for Cog ⚙️
# https://github.com/replicate/cog/blob/main/docs/python.md

from cog import BasePredictor, Input, Path


class Predictor(BasePredictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        # self.model = torch.load("./weights.pth")

    def predict(
        self,
        input: Path = Input(description="Grayscale input image"),
        scale: float = Input(
            description="Factor to scale image by", gt=0, lt=10, default=1.5
        ),
    ) -> Path:
        """Run a single prediction on the model"""
        # processed_input = preprocess(input)
        # output = self.model(processed_input, scale)
        # return postprocess(output)
