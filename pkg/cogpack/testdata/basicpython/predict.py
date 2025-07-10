from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, text: str = Input(description="Input text")) -> str:
        """Echo back a greeting."""
        return "hello " + text
