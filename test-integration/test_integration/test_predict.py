from pathlib import Path
import pytest
import subprocess

from .util import random_string


def test_predict_takes_string_inputs_and_returns_strings_to_stdout():
    project_dir = Path(__file__).parent / "fixtures/string-project"
    result = subprocess.run(
        ["cog", "predict", "-i", "world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == b"hello world\n"


def test_predict_runs_an_existing_image(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/string-project"
    image_name = "cog-test-" + random_string(10)

    try:
        subprocess.run(
            ["cog", "build", "-t", image_name],
            cwd=project_dir,
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


# https://github.com/replicate/cog/commit/28202b12ea40f71d791e840b97a51164e7be3b3c
# we need to find a better way to test this
@pytest.mark.skip("incredibly slow")
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


def test_predict_in_subdirectory_with_imports(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/subdirectory-project"
    result = subprocess.run(
        ["cog", "predict", "-i", "world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == b"hello world\n"
