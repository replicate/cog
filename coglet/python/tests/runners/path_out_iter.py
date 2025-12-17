import tempfile
import time
from typing import Iterator

from cog import BasePredictor, Path


class Predictor(BasePredictor):
    test_inputs = {'n': 2}

    def predict(self, n: int) -> Iterator[Path]:
        for i in range(n):
            time.sleep(1)
            with tempfile.NamedTemporaryFile(
                mode='w', suffix='.txt', delete=False
            ) as f:
                f.write(f'out{i}')
            yield Path(f.name)
