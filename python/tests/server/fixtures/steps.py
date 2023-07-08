import time


class Predictor:
    def setup(self):
        print("did setup")

    def predict(self, steps=5, name="Bob"):
        print("START")
        for i in range(steps):
            time.sleep(0.1)
            print(f"STEP {i+1}")
        print("END")
        return f"NAME={name}"
