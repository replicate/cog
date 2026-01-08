from typing import List, Optional

from cog import BasePredictor, Input

BAD_INPUTS = [
    (
        {'s': 'bar123'},
        "invalid input value: s='bar123' does not match regex 'foo.*'",
    ),
    (
        {'xs': ['bar123']},
        "invalid input value: xs=['bar123'] does not match regex 'foo.*'",
    ),
]


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self,
        s: Optional[str] = Input(regex='foo.*'),
        xs: List[str] = Input(regex='foo.*'),
    ) -> str:
        return 'foo'
