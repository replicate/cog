from cog import BasePredictor, Input

FIXTURE = [
    (
        {
            'int_ge': 0,
            'int_le': 10,
            'int_ge_le': 6,
            'float_ge': 2.71,
            'float_le': 3.14,
            'float_ge_le': 3.1,
            'float_ge_i': 0.0,
            'float_le_i': 10.0,
            'float_ge_le_i': 4.0,
        },
        '0,10,6,5,2.71,3.14,3.10,3.00,0.00,10.00,4.00,5.00',
    ),
    (
        {
            'int_ge': 0,
            'int_le': 10,
            'int_ge_le': 0,
            'int_ge_le_wd': 10,
            'float_ge': 2.71,
            'float_le': 3.14,
            'float_ge_le': 2.71,
            'float_ge_le_wd': 3.14,
            'float_ge_i': 0.0,
            'float_le_i': 10.0,
            'float_ge_le_i': 0.0,
            'float_ge_le_i_wd': 10.0,
        },
        '0,10,0,10,2.71,3.14,2.71,3.14,0.00,10.00,0.00,10.00',
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        int_ge: int = Input(ge=0),
        int_le: int = Input(le=10),
        int_ge_le: int = Input(ge=0, le=10),
        int_ge_le_wd: int = Input(default=5, ge=0, le=10),
        float_ge: float = Input(ge=2.71),
        float_le: float = Input(le=3.14),
        float_ge_le: float = Input(ge=2.71, le=3.14),
        float_ge_le_wd: float = Input(default=3.0, ge=2.71, le=3.14),
        float_ge_i: float = Input(ge=0),
        float_le_i: float = Input(le=10),
        float_ge_le_i: float = Input(ge=0, le=10),
        float_ge_le_i_wd: float = Input(default=5, ge=0, le=10),
    ) -> str:
        return ','.join(
            [
                f'{int_ge},{int_le},{int_ge_le},{int_ge_le_wd}',
                f'{float_ge:.2f},{float_le:.2f},{float_ge_le:.2f},{float_ge_le_wd:.2f}',
                f'{float_ge_i:.2f},{float_le_i:.2f},{float_ge_le_i:.2f},{float_ge_le_i_wd:.2f}',
            ]
        )
