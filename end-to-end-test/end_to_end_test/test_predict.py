import subprocess


def test_predict(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "predict.py", "w") as f:
        f.write(
            """
import cog

class Model(cog.Model):
    def setup(self):
        pass

    @cog.input("input", type=str)
    def predict(self, input):
        return "hello " + input
        """
        )
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
model: "predict.py:Model"
environment:
  python: "3.8"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        ["cog", "predict", "-i", "world"], cwd=tmpdir, check=True, capture_output=True
    )
    assert b"hello world" in result.stdout
