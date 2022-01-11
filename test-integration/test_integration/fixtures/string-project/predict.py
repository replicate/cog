import cog


class Predictor(cog.Predictor):
    def predict(self, input: str) -> str:
        return "hello " + input
