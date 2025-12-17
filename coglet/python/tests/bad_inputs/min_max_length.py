from typing import List, Optional

from cog import BasePredictor, Input

BAD_INPUTS = [
    (
        {'s': 'f'},
        "invalid input value: s='f' fails constraint len() >= 3",
    ),
    (
        {'s': 'foobar'},
        "invalid input value: s='foobar' fails constraint len() <= 5",
    ),
    (
        {'xs': ['f']},
        "invalid input value: xs=['f'] fails constraint len() >= 3",
    ),
    (
        {'xs': ['foobar']},
        "invalid input value: xs=['foobar'] fails constraint len() <= 5",
    ),
]


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self,
        s: Optional[str] = Input(min_length=3, max_length=5),
        xs: List[str] = Input(min_length=3, max_length=5),
    ) -> str:
        return 'foo'
