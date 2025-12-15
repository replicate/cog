import time
from typing import Iterator

from cog import BasePredictor


class Predictor(BasePredictor):
    test_inputs = {'i': 3, 's': 'foo'}

    def predict(self, i: int, s: str) -> Iterator[str]:
        time.sleep(0.1)
        print('starting prediction')
        if i > 0:
            time.sleep(0.6)
        for x in range(i):
            print(f'prediction in progress {x + 1}/{i}')
            time.sleep(0.6)
            yield f'*{s}-{x}*'
            time.sleep(0.6)
        print('completed prediction')
