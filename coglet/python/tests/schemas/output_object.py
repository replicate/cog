from cog import BaseModel, BasePredictor, Path, Secret


class Output(BaseModel):
    b: bool
    f: float
    i: int
    s: str
    path: Path
    secret: Secret


FIXTURE = [
    (
        {'b': False, 'f': 3.14, 'i': 1, 's': 'foo', 'path': 'foo.txt', 'secret': 'bar'},
        Output(
            b=False, f=3.14, i=1, s='foo', path=Path('foo.txt'), secret=Secret('bar')
        ),
    ),
    (
        {
            'b': True,
            'f': 2.71,
            'i': 2,
            's': 'bar',
            'path': Path('bar.txt'),
            'secret': Secret('baz'),
        },
        Output(
            b=True, f=2.71, i=2, s='bar', path=Path('bar.txt'), secret=Secret('baz')
        ),
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        b: bool,
        f: float,
        i: int,
        s: str,
        path: Path,
        secret: Secret,
    ) -> Output:
        return Output(b=b, f=f, i=i, s=s, path=path, secret=secret)
