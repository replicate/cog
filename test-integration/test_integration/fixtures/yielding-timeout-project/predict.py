from typing import Generator
import cog
import time


class Predictor(cog.Predictor):
    def predict(
        self, sleep_time: float, n_iterations: int
    ) -> Generator[str, None, None]:
        for i in range(n_iterations):
            time.sleep(sleep_time)
            yield f"yield {i}"
