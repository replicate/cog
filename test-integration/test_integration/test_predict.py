import pathlib
import shutil
import subprocess
from pathlib import Path

import pytest

from .util import random_string


def test_predict_takes_string_inputs_and_returns_strings_to_stdout():
    project_dir = Path(__file__).parent / "fixtures/string-project"
    result = subprocess.run(
        ["cog", "predict", "-i", "s=world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == b"hello world\n"


def test_predict_takes_int_inputs_and_returns_ints_to_stdout():
    project_dir = Path(__file__).parent / "fixtures/int-project"
    result = subprocess.run(
        ["cog", "predict", "-i", "num=2"],
        cwd=project_dir,
        check=True,
        capture_output=True,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == b"4\n"


def test_predict_takes_file_inputs(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/file-input-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    with open(out_dir / "input.txt", "w") as fh:
        fh.write("what up")
    result = subprocess.run(
        ["cog", "predict", "-i", "path=@" + str(out_dir / "input.txt")],
        cwd=out_dir,
        check=True,
        capture_output=True,
    )
    assert result.stdout == b"what up\n"


def test_predict_writes_files_to_files(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/file-output-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        ["cog", "predict"],
        cwd=out_dir,
        check=True,
        capture_output=True,
    )
    assert result.stdout == b""
    with open(out_dir / "output.bmp", "rb") as f:
        assert len(f.read()) == 195894


def test_predict_writes_files_to_files_with_custom_name(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/file-output-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        ["cog", "predict", "-o", out_dir / "myoutput.bmp"],
        cwd=out_dir,
        check=True,
        capture_output=True,
    )
    assert result.stdout == b""
    with open(out_dir / "myoutput.bmp", "rb") as f:
        assert len(f.read()) == 195894


def test_predict_writes_multiple_files_to_files(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/file-list-output-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        [
            "cog",
            "predict",
        ],
        cwd=out_dir,
        check=True,
        capture_output=True,
    )
    assert result.stdout == b""
    with open(out_dir / "output.0.txt") as f:
        assert f.read() == "foo"
    with open(out_dir / "output.1.txt") as f:
        assert f.read() == "bar"
    with open(out_dir / "output.2.txt") as f:
        assert f.read() == "baz"


def test_predict_writes_strings_to_files(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/string-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    result = subprocess.run(
        ["cog", "predict", "-i", "s=world", "-o", out_dir / "out.txt"],
        cwd=project_dir,
        check=True,
        capture_output=True,
    )
    assert result.stdout == b""
    with open(out_dir / "out.txt") as f:
        assert f.read() == "hello world"


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
            ["cog", "predict", image_name, "-i", "s=world"],
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
    image_name = "r8.im/replicate/hello-world@sha256:5c7d5dc6dd8bf75c1acaa8565735e7986bc5b66206b55cca93cb72c9bf15ccaa"
    subprocess.run(["docker", "rmi", image_name], check=False)

    # Run in another directory to ensure it doesn't use cog.yaml
    another_directory = tmpdir_factory.mktemp("project")
    result = subprocess.run(
        ["cog", "predict", image_name, "-i", "text=world"],
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
        ["cog", "predict", "-i", "s=world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == b"hello world\n"


def test_predict_many_inputs(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/many-inputs-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    inputs = {
        "no_default": "hello",
        "path": "@path.txt",
        "image": "@image.jpg",
        "choices": "foo",
        "int_choices": 3,
    }
    with open(out_dir / "path.txt", "w") as fh:
        fh.write("world")
    with open(out_dir / "image.jpg", "w") as fh:
        fh.write("")
    cmd = ["cog", "predict"]

    for k, v in inputs.items():
        cmd += ["-i", f"{k}={v}"]

    result = subprocess.run(
        cmd,
        cwd=out_dir,
        check=True,
        capture_output=True,
    )
    assert result.stdout.decode() == "hello default 20 world jpg foo 6\n"
