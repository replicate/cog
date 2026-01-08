from enum import Enum

from cog import BasePredictor, Input


class Colors(str, Enum):
    RED = 'red'
    GREEN = 'green'
    BLUE = 'blue'


class Numbers(float, Enum):
    E = 2.71828
    PHI = 1.618
    PI = 3.14


FIXTURE = [
    (
        {},
        'red:FF0000,2.71828:epsilon',
    ),
    (
        {'c': Colors.GREEN, 'n': Numbers.PHI},
        'green:00FF00,1.618:phi',
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        c: str = Input(default=Colors.RED, choices=[c.value for c in Colors]),
        n: float = Input(default=Numbers.E),
    ) -> str:
        cs = ''
        if c == Colors.RED:
            cs = 'FF0000'
        elif c == Colors.GREEN:
            cs = '00FF00'
        elif c == Colors.BLUE:
            cs = '0000FF'
        ns = ''
        if n == Numbers.E:
            ns = 'epsilon'
        elif n == Numbers.PHI:
            ns = 'phi'
        elif n == Numbers.PI:
            ns = 'pi'
        return f'{c}:{cs},{n}:{ns}'
