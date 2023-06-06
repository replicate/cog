import os
import tempfile

from cog import BasePredictor, Path
from PIL import Image


class Predictor(BasePredictor):
    def predict(self) -> Path:
        temp_dir = tempfile.mkdtemp()
        temp_path = os.path.join(temp_dir, "my_file.bmp")
        img = Image.new("RGB", (255, 255), "red")
        img.save(temp_path)
        return Path(temp_path)
