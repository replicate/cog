import time

from tqdm import tqdm

from cog import BasePredictor


class Predictor(BasePredictor):
    test_inputs = {'s': 'foo'}

    def setup(self) -> None:
        print('starting async setup')
        for _ in tqdm(range(500)):
            time.sleep(0.01)
        print('completed async setup')

    def predict(self, s: str) -> str:
        print('starting async prediction')
        for _ in tqdm(range(500)):
            time.sleep(0.01)
        print('completed async prediction')
        return f'*{s}*'
