from time import sleep

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        sleep(0.2)  # sleep to help test timing
        yield f"hello {text}"
        sleep(0.2)  # sleep to help test timing
        print("a printed log message")
        sleep(0.2)  # sleep to help test timing
        raise Exception("mid run error")
