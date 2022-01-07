import cog


class Predictor(cog.Predictor):
    @cog.input("input", type=str)
    def predict(self, input):
        return "hello " + input
