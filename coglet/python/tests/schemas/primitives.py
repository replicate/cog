from cog import BasePredictor

FIXTURE = [
    ({'b': False, 'f': 2.71, 'i': 0, 's': 'foo'}, 'False,2.71,0,foo'),
    ({'b': True, 'f': 3.14, 'i': 1, 's': 'bar'}, 'True,3.14,1,bar'),
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
    ) -> str:
        return f'{b},{f:.2f},{i},{s}'
