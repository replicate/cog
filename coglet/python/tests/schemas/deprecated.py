from cog import BasePredictor, Input

FIXTURE = [
    (
        {
            's': 'foo',
        },
        'foo',
    )
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        s: str = Input(description='deprecated field', deprecated=True),
    ) -> str:
        return s
