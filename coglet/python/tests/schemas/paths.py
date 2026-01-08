from typing import List

from cog import BasePredictor, Input, Path

FIXTURE = [
    (
        {
            'p1': 'foo.txt',
        },
        [
            Path(x)
            for x in ['foo.txt', 'bar.txt', 'baz.txt', 'bar123.txt', 'baz123.txt']
        ],
    ),
    (
        {'p1': 'foo.png', 'p2': 'bar.png', 'p3': Path('baz.png')},
        [
            Path(x)
            for x in ['foo.png', 'bar.png', 'baz.png', 'bar123.txt', 'baz123.txt']
        ],
    ),
    (
        {
            'p1': 'foo.jpg',
            'p2': 'bar.jpg',
            'p3': Path('baz.jpg'),
            'ps': [Path('bar321.jpg'), Path('baz321.jpg')],
        },
        [
            Path(x)
            for x in ['foo.jpg', 'bar.jpg', 'baz.jpg', 'bar321.jpg', 'baz321.jpg']
        ],
    ),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        p1: Path,
        p2: Path = Input(default=Path('bar.txt')),
        p3: Path = Input(default='baz.txt'),
        ps: List[Path] = Input(
            default_factory=lambda: ['bar123.txt', Path('baz123.txt')]
        ),
    ) -> List[Path]:
        return [p1, p2, p3] + ps
