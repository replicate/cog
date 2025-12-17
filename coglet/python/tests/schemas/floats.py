from typing import List

from cog import BasePredictor, Input

FIXTURE = [
    ({}, '3.00,3.14,[3.00,4.00],[2.71,3.14]'),
    (
        {
            'f1': 1,
            'f2': 2.71,
            'f3': [1, 2],
            'f4': [1.1, 2.2],
        },
        '1.00,2.71,[1.00,2.00],[1.10,2.20]',
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        f1: float = Input(default=3),
        f2: float = Input(default=3.14),
        f3: List[float] = Input(default_factory=lambda: [3, 4]),
        f4: List[float] = Input(default_factory=lambda: [2.71, 3.14]),
    ) -> str:
        def f(xs):
            return ','.join(f'{x:.2f}' for x in xs)

        return f'{f1:.2f},{f2:.2f},[{f(f3)}],[{f(f4)}]'
