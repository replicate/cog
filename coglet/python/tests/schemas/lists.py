from typing import List

from cog import BasePredictor, Input

FIXTURE = [
    (
        {
            'f1': [1.0],
            'f2': [2.0],
            'i1': [3],
            'i2': [4],
        },
        '[1.00],[2.00],[3],[4],[True,False],[2.71,3.14],[0,1,2],[foo,bar]',
    ),
    (
        {
            'f1': [1.0],
            'f2': [2.0],
            'i1': [3],
            'i2': [4],
            'b_wd': [False, True],
            'f_wd': [1.1, 2.2],
            'i_wd': [1, 2, 3],
            's_wd': ['foo', 'baz'],
        },
        '[1.00],[2.00],[3],[4],[False,True],[1.10,2.20],[1,2,3],[foo,baz]',
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        f1: List[float],
        f2: list[float],
        i1: List[int],
        i2: list[int],
        b_wd: List[bool] = Input(default_factory=lambda: [True, False]),
        f_wd: List[float] = Input(default_factory=lambda: [2.71, 3.14]),
        i_wd: List[int] = Input(default_factory=lambda: [0, 1, 2]),
        s_wd: List[str] = Input(default_factory=lambda: ['foo', 'bar']),
    ) -> str:
        def f2s(xs):
            return ','.join(f'{x:.2f}' for x in xs)

        def o2s(xs):
            return ','.join(str(x) for x in xs)

        return f'[{f2s(f1)}],[{f2s(f2)}],[{o2s(i1)}],[{o2s(i2)}],[{o2s(b_wd)}],[{f2s(f_wd)}],[{o2s(i_wd)}],[{o2s(s_wd)}]'
