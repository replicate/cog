from cog import BasePredictor
import threading


def keep_printing():
    for _ in range(10000):
        print("hello")


class Predictor(BasePredictor):
    def setup(self):
        self.print_thread = threading.Thread(target=keep_printing)

    def predict(self) -> str:
        self.print_thread.start()
        output = "output" * 100000  # bigger output increases the chance of race condition
        return output
