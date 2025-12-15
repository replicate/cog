from cog import BasePredictor, Input

FIXTURE = [
    (
        {
            'r': 'https://r8.im',
        },
        'https://r8.im|https://replicate.com',
    ),
    (
        {
            'r': 'https://r8.im',
            'r_wd': 'https://github.com/replicate',
        },
        'https://r8.im|https://github.com/replicate',
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        r: str = Input(regex='https://.*'),
        r_wd: str = Input(regex='https://.*', default='https://replicate.com'),
    ) -> str:
        return f'{r}|{r_wd}'
