from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, s: str = Input(description="My Input Description", default=None)) -> str:
        return "hello " + s
