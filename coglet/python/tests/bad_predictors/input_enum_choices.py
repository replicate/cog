from enum import Enum

from cog import BasePredictor, Input

ERROR = 'enum Colors is used as str but does not extend it'


class Colors(Enum):
    RED = 'red'
    GREEN = 'green'
    BLUE = 'blue'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, c: str = Input(choices=list(Colors))) -> str:
        pass
