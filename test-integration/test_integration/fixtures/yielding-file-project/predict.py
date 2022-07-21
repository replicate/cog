from time import sleep
from typing import Iterator

from cog import BasePredictor, Path


class Predictor(BasePredictor):
    def predict(self, path: Path) -> Iterator[Path]:
        with open(path) as f:
            prefix = f.read()

        predictions = ["foo", "bar", "baz"]
        for i, prediction in enumerate(predictions):
            sleep(0.2)  # sleep to help test timing
            out_path = Path(f"/tmp/out-{i}.txt")
            with out_path.open("w") as f:
                f.write(prefix + " " + prediction)
            yield out_path
