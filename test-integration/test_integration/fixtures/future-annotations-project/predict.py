# predict.py - This will FAIL with the TypeError
from __future__ import annotations

from cog import BasePredictor, Input, Path
from PIL import Image

class Predictor(BasePredictor):
    def predict(
        self,
        image: Path = Input(description="Input image"),
    ) -> Path:
        """Simple pass-through prediction that exhibits the bug."""
        # This would normally just return the input image
        img = Image.open(image)
        output_path = Path("/tmp/output.png")
        img.save(output_path)
        return output_path
