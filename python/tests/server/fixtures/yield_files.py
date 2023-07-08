import os
import tempfile
from typing import Iterator

from cog import BasePredictor, Path
from PIL import Image


class Predictor(BasePredictor):
    def predict(self) -> Iterator[Path]:
        colors = ["red", "blue", "yellow"]
        for i, color in enumerate(colors):
            temp_dir = tempfile.mkdtemp()
            temp_path = os.path.join(temp_dir, f"prediction-{i}.bmp")
            img = Image.new("RGB", (255, 255), color)
            img.save(temp_path)
            yield Path(temp_path)
