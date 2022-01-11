from PIL import Image
import os
import tempfile

import cog

class Predictor(cog.Predictor):
    def predict(self) -> cog.Path:
        temp_dir = tempfile.mkdtemp()
        temp_path = os.path.join(temp_dir, f"prediction.bmp")
        img = Image.new("RGB", (255, 255), "red")
        img.save(temp_path)
        return cog.Path(temp_path)
