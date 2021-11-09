import subprocess

from .util import random_string


def test_predict(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "predict.py", "w") as f:
        f.write(
            """
import cog

class Predictor(cog.Predictor):
    def setup(self):
        pass

    @cog.input("input", type=str)
    def predict(self, input):
        return "hello " + input
        """
        )
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.8"
predict: "predict.py:Predictor"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        ["cog", "predict", "-i", "world"], cwd=tmpdir, check=True, capture_output=True
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == b"hello world\n"


def test_predict_with_existing_image(tmpdir_factory):
    image_name = "cog-test-" + random_string(10)
    try:
        tmpdir = tmpdir_factory.mktemp("project")
        with open(tmpdir / "predict.py", "w") as f:
            f.write(
                """
import cog

class Predictor(cog.Predictor):
    def setup(self):
        pass

    @cog.input("input", type=str)
    def predict(self, input):
        return "hello " + input
        """
            )
        with open(tmpdir / "cog.yaml", "w") as f:
            cog_yaml = """
build:
  python_version: "3.8"
predict: "predict.py:Predictor"
            """
            f.write(cog_yaml)

        subprocess.run(
            ["cog", "build", "-t", image_name],
            cwd=tmpdir,
            check=True,
        )

        # Run in another directory to ensure it doesn't use cog.yaml
        another_directory = tmpdir_factory.mktemp("project")
        result = subprocess.run(
            ["cog", "predict", image_name, "-i", "world"],
            cwd=another_directory,
            check=True,
            capture_output=True,
        )
        assert result.stdout == b"hello world\n"
    finally:
        subprocess.run(["docker", "rmi", image_name], check=True)


def test_predict_with_remote_image(tmpdir_factory):
    image_name = "r8.im/bfirsh/hello-world@sha256:942f3080b0307e926646c6be51f9762991a2d5411b9fd8ee98a6dcc25bcaa9b9"
    subprocess.run(["docker", "rmi", image_name], check=False)

    # Run in another directory to ensure it doesn't use cog.yaml
    another_directory = tmpdir_factory.mktemp("project")
    result = subprocess.run(
        ["cog", "predict", image_name, "-i", "world"],
        cwd=another_directory,
        check=True,
        capture_output=True,
    )

    out = result.stdout.decode()

    # lots of docker pull logs are written to stdout before writing the actual output
    # TODO: clean up docker output so cog predict is always clean
    assert out.strip().endswith("hello world")


def test_predict_multiple_outputs(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "predict.py", "w") as f:
        f.write(
            """
import cog
import tempfile
from pathlib import Path

class Predictor(cog.Predictor):
    def setup(self):
        pass

    @cog.input("input", type=str)
    def predict(self, input):
        out_dir = Path(tempfile.mkdtemp())
        out_path1 = out_dir / "first.txt"
        out_path2 = out_dir / "second.txt"
        with open(out_path1, "w") as f:
            f.write(f"one {input}")
        with open(out_path2, "w") as f:
            f.write(f"two {input}")
        
        return {"first": out_path1, "second": out_path2, "third": "just text"}
        """
        )
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.8"
predict: "predict.py:Predictor"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        ["cog", "predict", "-i", "hello"], cwd=tmpdir, check=True, capture_output=True
    )
    stdout = result.stdout.decode()

    # multi-output case is less clean than single-output, and
    # outputs can be printed in any order
    expected_strings = ["first:\none hello\n", "second:\ntwo hello\n", "third:\njust text\n"]
    for expected_string in expected_strings:
        assert expected_string in stdout

    assert len(stdout) == sum([len(s) for s in expected_strings])
