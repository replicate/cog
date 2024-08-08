import threading
from typing import Iterator

from cog import BasePredictor


def keep_printing():
    for _ in range(10000):
        print("hello")


class Predictor(BasePredictor):
    def predict(self) -> Iterator[str]:
        print_thread = threading.Thread(target=keep_printing)
        print_thread.start()
        yield "output" * 10000
        yield "output" * 10000
        yield "output" * 10000
        print_thread.join()
