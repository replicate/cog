from typing import List

from cog import BasePredictor, ConcatenateIterator

FIXTURE = [
    ({'xs': []}, []),
    ({'xs': ['foo', 'bar', 'baz']}, ['*foo*', '*bar*', '*baz*']),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, xs: List[str]) -> ConcatenateIterator[str]:
        for x in xs:
            yield f'*{x}*'
