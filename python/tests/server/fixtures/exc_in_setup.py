class Predictor:
    def setup(self):
        raise RuntimeError("setup error")

    def predict(self):
        print("did predict")
