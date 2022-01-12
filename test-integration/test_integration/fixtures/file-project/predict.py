from pathlib import Path
import tempfile
import cog


class Predictor(cog.Predictor):
    def setup(self):
        self.foo = "foo"

    @cog.input("text", type=str)
    @cog.input("path", type=Path)
    def predict(self, text, path):
        with open(path) as f:
            output = self.foo + text + f.read()
        tmpdir = Path(tempfile.mkdtemp())
        with open(tmpdir / "output.txt", "w") as fh:
            fh.write(output)
        return tmpdir / "output.txt"
