from cog import BasePredictor, Path, Input
from PIL import Image


class Predictor(BasePredictor):
    def predict(self,test_image: Path | None = Input(description="Test image", default=None)) -> Path:
        """Run a single prediction on the model"""
        im = Image.new("RGB", (100, 100), color="red")
        im.save(Path("./hello.webp"))
        return Path("./hello.webp")
    