class Predictor:
    def setup(self):
        pass

    def predict(self, upto):
        for i in range(upto):
            yield i
