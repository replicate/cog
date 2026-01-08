from typing import List, Optional

from cog import BasePredictor, Input

BAD_INPUTS = [
    (
        {'s': 'baz'},
        "invalid input value: s='baz' does not match choices ['foo', 'bar']",
    ),
    (
        {'xs': ['baz']},
        "invalid input value: xs=['baz'] does not match choices ['foo', 'bar']",
    ),
]


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(
        self,
        s: Optional[str] = Input(choices=['foo', 'bar']),
        xs: List[str] = Input(choices=['foo', 'bar']),
    ) -> str:
        return 'foo'
