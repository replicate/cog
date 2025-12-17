from cog import BasePredictor, Input

FIXTURE = [
    (
        {
            's_min': 'fooba',
            's_max': 'foobarbaz0',
            's_min_max': 'foobar1',
        },
        'fooba,foobarbaz0,foobar1,foobar',
    ),
    (
        {
            's_min': 'fooba',
            's_max': 'foobarbaz0',
            's_min_max': 'fooba',
            's_min_max_wd': 'foobarbaz1',
        },
        'fooba,foobarbaz0,fooba,foobarbaz1',
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        s_min: str = Input(min_length=5),
        s_max: str = Input(max_length=10),
        s_min_max: str = Input(min_length=5, max_length=10),
        s_min_max_wd: str = Input(default='foobar', min_length=5, max_length=10),
    ) -> str:
        return f'{s_min},{s_max},{s_min_max},{s_min_max_wd}'
