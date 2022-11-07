import time

class Predictor:
    def setup(self):
        pass

    def predict(self, sleep=0):
        time.sleep(sleep)
        return f"done in {sleep} seconds"
