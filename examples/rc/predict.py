from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, text: str = Input(description="Input text")) -> str:
        return text.upper()
