from typing import List, Optional

from cog import BasePredictor, Input

BAD_INPUTS = [
    (
        {'i': -1},
        'invalid input value: i=-1 fails constraint >= 0.0',
    ),
    (
        {'i': 100},
        'invalid input value: i=100 fails constraint <= 10.0',
    ),
    (
        {'xs': [-1]},
        'invalid input value: xs=[-1] fails constraint >= 0.0',
    ),
    (
        {'xs': [100]},
        'invalid input value: xs=[100] fails constraint <= 10.0',
    ),
]


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self, i: Optional[int] = Input(ge=0, le=10), xs: List[int] = Input(ge=0, le=10)
    ) -> str:
        return 'foo'
