import cog

from mylib import concat


class Predictor(cog.Predictor):
    @cog.input("input", type=str)
    def predict(self, input):
        return concat("hello", input)
