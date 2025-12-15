from cog import BasePredictor, Input, Path

FIXTURE = [
    ({}, 'False,0.00,0,foo,foo.txt'),
    (
        {
            'b': True,
            'f': 3.14,
            'i': 1,
            's': 'bar',
            'p': Path('bar.txt'),
        },
        'True,3.14,1,bar,bar.txt',
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        b: bool = Input(default=False),
        f: float = Input(default=0.0),
        i: int = Input(default=0),
        s: str = Input(default='foo'),
        p: Path = Input(default='foo.txt'),
    ) -> str:
        return f'{b},{f:.2f},{i},{s},{p}'
