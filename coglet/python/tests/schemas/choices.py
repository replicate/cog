from cog import BasePredictor, Input

FIXTURE = [
    (
        {
            'i': 0,
            's': 'foo',
        },
        '0,0,foo,foo',
    ),
    (
        {
            'i': 2,
            's': 'bar',
        },
        '2,0,bar,foo',
    ),
    (
        {
            'i': 0,
            'i_wd': 0,
            's': 'foo',
            's_wd': 'foo',
        },
        '0,0,foo,foo',
    ),
    (
        {
            'i': 2,
            'i_wd': 2,
            's': 'bar',
            's_wd': 'bar',
        },
        '2,2,bar,bar',
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        i: int = Input(choices=[0, 1, 2]),
        i_wd: int = Input(choices=[0, 1, 2], default=0),
        s: str = Input(choices=['foo', 'bar']),
        s_wd: str = Input(choices=['foo', 'bar'], default='foo'),
    ) -> str:
        return f'{i},{i_wd},{s},{s_wd}'
