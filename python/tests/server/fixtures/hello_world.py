class Predictor:
    def setup(self):
        print("did setup")

    def predict(self, name):
        print(f"hello, {name}")
        return f"hello, {name}"
