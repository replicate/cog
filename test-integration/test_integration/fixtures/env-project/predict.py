from cog import BasePredictor
import os


class Predictor(BasePredictor):
    def predict(self, name: str) -> str:
        return f"ENV[{name}]={os.getenv(name)}"
