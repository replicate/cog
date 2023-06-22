import os
import tempfile

from cog import BasePredictor, Path

class Predictor(BasePredictor):
    def predict(self) -> Path:
        temp_dir = tempfile.mkdtemp()
        temp_path = os.path.join(temp_dir, "prediction.bmp")

        # 1x1 black bitmap
        bmp = bytes.fromhex("424D1E000000000000001A0000000C000000010001000100180000000000")

        with open(temp_path, "wb") as file:
            file.write(bmp)

        return Path(temp_path)
