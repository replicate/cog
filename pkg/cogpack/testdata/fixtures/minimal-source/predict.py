from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, text: str = Input(description="Text to echo back")) -> str:
        """Echo the input text back - simple test of source copy functionality."""
        return f"Echo: {text}"