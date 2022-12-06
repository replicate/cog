import os
import tempfile

from cog import BasePredictor, Path


class Predictor(BasePredictor):
    def predict(self) -> Path:
        temp_dir = tempfile.mkdtemp()
        temp_path = os.path.join(temp_dir, "file.txt")
        with open(temp_path, "w") as fh:
            fh.write("hello")
        return Path(temp_path)
