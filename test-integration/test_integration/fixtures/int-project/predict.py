import cog


class Predictor(cog.Predictor):
    def predict(self, input: int) -> int:
        return input * 2
