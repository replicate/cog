import time


class Predictor:
    def setup(self):
        print("did setup")

    def predict(self):
        for _ in range(10):
            print("doing stuff")
            time.sleep(3)
        print("did predict")
