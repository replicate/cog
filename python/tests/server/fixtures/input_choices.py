from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, text: str = Input(choices=["foo", "bar"])) -> str:
        assert type(text) == str
        return text
