from cog import BasePredictor, Input, Path

FIXTURE = [
    (
        {
            'b': True,
            'f': 3.14,
            'i': 1,
            's': 'foo',
            'p': 'foo.txt',
        },
        'True,3.14,1,foo,foo.txt',
    )
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        b: bool = Input(description='boolean field'),
        f: float = Input(description='float field'),
        i: int = Input(description='integer field'),
        s: str = Input(description='string field'),
        p: Path = Input(description='path field'),
    ) -> str:
        return f'{b},{f:.2f},{i},{s},{p}'
