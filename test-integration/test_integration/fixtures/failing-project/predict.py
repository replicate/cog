import cog


class Predictor(cog.Predictor):
    def predict(self, text: str):
        raise Exception("over budget")
