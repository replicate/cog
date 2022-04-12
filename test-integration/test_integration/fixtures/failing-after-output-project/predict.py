import time

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        yield f"hello {text}"
        time.sleep(0.5)
        print("a printed log message")
        time.sleep(0.5)
        raise Exception("mid run error")
