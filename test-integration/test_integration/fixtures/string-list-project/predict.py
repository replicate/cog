from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, s: list[str] = Input(description="A list of strings to print.")) -> str:
        return "hello " + "|".join(s)
