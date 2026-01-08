import os
import tempfile

from cog import BasePredictor


class MyPath(os.PathLike):
    def __init__(self, path: str) -> None:
        # Store path components internally
        self._path = path

    def __fspath__(self) -> str:
        # Build and return a string path
        return self._path

    def __repr__(self) -> str:
        return f'MyPath({self._path!r})'


class Predictor(BasePredictor):
    test_inputs = {'s': 'hello'}

    def predict(self, s: str) -> MyPath:
        with tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False) as f:
            f.write(f'*{s}*')
        return MyPath(f.name)
