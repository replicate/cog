import time
from typing import Generator

from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(
        self, sleep_time: float, n_iterations: int
    ) -> Generator[str, None, None]:
        for i in range(n_iterations):
            time.sleep(sleep_time)
            yield f"yield {i}"
