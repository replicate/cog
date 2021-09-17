import cog

class Predictor(cog.Predictor):
    def setup(self):
        pass

    @cog.input("text", type=str)
    def predict(self, text):
        predictions = ["foo", "bar", "baz"]
        for prediction in predictions:
            yield prediction
