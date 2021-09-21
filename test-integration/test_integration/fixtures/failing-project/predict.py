import cog


class Predictor(cog.Predictor):
    def setup(self):
        pass

    @cog.input("text", type=str)
    def predict(self, text):
        raise Exception("over budget")
