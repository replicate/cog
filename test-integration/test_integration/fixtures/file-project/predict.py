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
        tmp = tempfile.NamedTemporaryFile(suffix=".txt")
        tmp.close()
        tmp_path = Path(tmp.name)
        with tmp_path.open("w") as f:
            f.write(output)
            return tmp_path
