import tempfile
import cog
from cog import Path


class Predictor(cog.Predictor):
    def setup(self):
        self.foo = "foo"

    def predict(self, text: str, path: Path) -> Path:
        with open(path) as f:
            output = self.foo + text + f.read()
        tmpdir = Path(tempfile.mkdtemp())
        with open(tmpdir / "output.txt", "w") as fh:
            fh.write(output)
        return tmpdir / "output.txt"
