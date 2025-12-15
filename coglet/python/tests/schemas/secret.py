from typing import List

from cog import BasePredictor, Input, Secret

FIXTURE = [
    (
        {
            's1': 'foo',
        },
        [Secret(x) for x in ['foo', 'bar', 'baz', 'bar123', 'baz123']],
    ),
    (
        {'s1': 'foo1', 's2': 'bar1', 's3': Secret('baz1')},
        [Secret(x) for x in ['foo1', 'bar1', 'baz1', 'bar123', 'baz123']],
    ),
    (
        {
            's1': 'foo2',
            's2': 'bar2',
            's3': Secret('baz2'),
            'ss': [Secret('bar321'), Secret('baz321')],
        },
        [Secret(x) for x in ['foo2', 'bar2', 'baz2', 'bar321', 'baz321']],
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        s1: Secret,
        s2: Secret = Input(default=Secret('bar')),
        s3: Secret = Input(default='baz'),
        ss: List[Secret] = Input(default_factory=lambda: ['bar123', Secret('baz123')]),
    ) -> List[Secret]:
        return [s1, s2, s3] + ss
