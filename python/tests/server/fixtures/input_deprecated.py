from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, text: str = Input(description="Some deprecated text", deprecated=True)) -> str:
        assert type(text) == str
        return text
