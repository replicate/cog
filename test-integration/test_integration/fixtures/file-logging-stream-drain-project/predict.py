from cog import BasePredictor
import tqdm
import time


class Predictor(BasePredictor):

    def predict(self) -> str:
        print("Starting Predict:")
        for i in tqdm.tqdm(range(10), total=10):
            time.sleep(i)
        return "Hello World"
