from typing import List

from cog import BasePredictor

BAD_INPUTS = [
    ({}, 'missing required input field: i'),
    (
        {'i': 0},
        'missing required input field: s',
    ),
    (
        {'s': 'foo'},
        'missing required input field: i',
    ),
    (
        {'i': 0, 's': 'foo'},
        'missing required input field: xs',
    ),
    # Allow unknown inputs for backwards compatibility
    # Print a warning instead as part of logs
    # ({'x': 0}, 'unknown input field: x'),
]


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, i: int, s: str, xs: List[str]) -> str:
        return 'foo'
