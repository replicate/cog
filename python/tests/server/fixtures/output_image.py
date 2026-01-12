import tempfile
from pathlib import Path

from cog import BasePredictor, Image


class Predictor(BasePredictor):
    def predict(self) -> Image:
        output_path = Path(tempfile.mkdtemp()) / "output.jpg"
        with open(output_path, "w") as fh:
            fh.write("test image data")
        return Image(output_path)
