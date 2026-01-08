from typing import Callable

from cog import BasePredictor

FIXTURE = [
    ({'s': 'foo'}, 'foo'),
]


def faster(f: Callable) -> Callable:
    def wrapper(*args, **kwargs):
        print('faster')
        return f(*args, **kwargs)

    return wrapper


def cheaper(f: Callable) -> Callable:
    def wrapper(*args, **kwargs):
        print('cheaper')
        return f(*args, **kwargs)

    return wrapper


class Predictor(BasePredictor):
    setup_done = False

    @faster
    @cheaper
    def setup(self) -> None:
        self.setup_done = True

    @cheaper
    @faster
    def predict(self, s: str) -> str:
        return f'{s}'
