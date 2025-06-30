# Prediction interface for Cog ⚙️
# https://github.com/replicate/cog/blob/main/docs/python.md

from cog import BasePredictor, Path


class Predictor(BasePredictor):
    def setup(self) -> None:
        """Load the model into memory to make running multiple predictions efficient"""
        if not Path("mesh.glb").exists():
            raise ValueError("Example file mesh.glb does not exist")

    def predict(
        self,
    ) -> Path:
        """Run a single prediction on the model"""
        return Path("mesh.glb")
