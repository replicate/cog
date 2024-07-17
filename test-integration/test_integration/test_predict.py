import pathlib
import shutil
import subprocess
from pathlib import Path

import pytest

DEFAULT_TIMEOUT = 60


def test_predict_takes_string_inputs_and_returns_strings_to_stdout():
    project_dir = Path(__file__).parent / "fixtures/string-project"
    result = subprocess.run(
        ["cog", "predict", "--debug", "-i", "s=world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == "hello world\n"
    assert "cannot use fast loader as current Python <3.9" in result.stderr
    assert "falling back to slow loader" in result.stderr


def test_predict_takes_int_inputs_and_returns_ints_to_stdout():
    project_dir = Path(__file__).parent / "fixtures/int-project"
    result = subprocess.run(
        ["cog", "predict", "--debug", "-i", "num=2"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == "4\n"
    assert "falling back to slow loader" not in result.stderr


def test_predict_takes_file_inputs(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/path-input-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    with open(out_dir / "input.txt", "w", encoding="utf-8") as fh:
        fh.write("what up")
    result = subprocess.run(
        ["cog", "predict", "--debug", "-i", "path=@" + str(out_dir / "input.txt")],
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.stdout == "what up\n"
    assert "falling back to slow loader" not in result.stderr


def test_predict_writes_files_to_files(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/path-output-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        ["cog", "predict", "--debug"],
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.stdout == ""
    with open(out_dir / "output.bmp", "rb") as f:
        assert len(f.read()) == 195894
    assert "falling back to slow loader" not in result.stderr


def test_predict_writes_files_to_files_with_custom_name(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/path-output-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        ["cog", "predict", "--debug", "-o", out_dir / "myoutput.bmp"],
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.stdout == ""
    with open(out_dir / "myoutput.bmp", "rb") as f:
        assert len(f.read()) == 195894
    assert "falling back to slow loader" not in result.stderr


def test_predict_writes_multiple_files_to_files(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/path-list-output-project"

    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)

    result = subprocess.run(
        ["cog", "predict"],
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )

    assert result.stdout == ""
    with open(out_dir / "output.0.txt", encoding="utf-8") as f:
        assert f.read() == "foo"
    with open(out_dir / "output.1.txt", encoding="utf-8") as f:
        assert f.read() == "bar"
    with open(out_dir / "output.2.txt", encoding="utf-8") as f:
        assert f.read() == "baz"
    assert "falling back to slow loader" not in result.stderr


def test_predict_writes_strings_to_files(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/string-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    result = subprocess.run(
        ["cog", "predict", "--debug", "-i", "s=world", "-o", out_dir / "out.txt"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.stdout == ""
    with open(out_dir / "out.txt", encoding="utf-8") as f:
        assert f.read() == "hello world"
    assert "cannot use fast loader as current Python <3.9" in result.stderr
    assert "falling back to slow loader" in result.stderr


def test_predict_runs_an_existing_image(docker_image, tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/string-project"

    subprocess.run(
        ["cog", "build", "--debug", "-t", docker_image],
        cwd=project_dir,
        check=True,
    )

    # Run in another directory to ensure it doesn't use cog.yaml
    another_directory = tmpdir_factory.mktemp("project")
    result = subprocess.run(
        ["cog", "predict", "--debug", docker_image, "-i", "s=world"],
        cwd=another_directory,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.stdout == "hello world\n"
    assert "cannot use fast loader as current Python <3.9" in result.stderr
    assert "falling back to slow loader" in result.stderr


# https://github.com/replicate/cog/commit/28202b12ea40f71d791e840b97a51164e7be3b3c
# we need to find a better way to test this
@pytest.mark.skip("incredibly slow")
def test_predict_with_remote_image(tmpdir_factory):
    image_name = "r8.im/replicate/hello-world@sha256:5c7d5dc6dd8bf75c1acaa8565735e7986bc5b66206b55cca93cb72c9bf15ccaa"
    subprocess.run(["docker", "rmi", "-f", image_name], check=True)

    # Run in another directory to ensure it doesn't use cog.yaml
    another_directory = tmpdir_factory.mktemp("project")
    result = subprocess.run(
        ["cog", "predict", image_name, "-i", "text=world"],
        cwd=another_directory,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )

    # lots of docker pull logs are written to stdout before writing the actual output
    # TODO: clean up docker output so cog predict is always clean
    assert result.stdout.strip().endswith("hello world")


def test_predict_in_subdirectory_with_imports(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/subdirectory-project"
    result = subprocess.run(
        ["cog", "predict", "--debug", "-i", "s=world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == "hello world\n"
    assert "falling back to slow loader" not in result.stderr


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
    with open(out_dir / "path.txt", "w", encoding="utf-8") as fh:
        fh.write("world")
    with open(out_dir / "image.jpg", "w", encoding="utf-8") as fh:
        fh.write("")
    cmd = ["cog", "--debug", "predict"]

    for k, v in inputs.items():
        cmd += ["-i", f"{k}={v}"]

    result = subprocess.run(
        cmd,
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.stdout == "hello default 20 world jpg foo 6\n"
    assert "falling back to slow loader" not in result.stderr


def test_predict_many_inputs_with_existing_image(docker_image, tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/many-inputs-project"

    subprocess.run(
        ["cog", "build", "--debug", "-t", docker_image],
        cwd=project_dir,
        check=True,
    )

    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    inputs = {
        "no_default": "hello",
        "path": "@path.txt",
        "image": "@image.jpg",
        "choices": "foo",
        "int_choices": 3,
    }
    with open(out_dir / "path.txt", "w", encoding="utf-8") as fh:
        fh.write("world")
    with open(out_dir / "image.jpg", "w", encoding="utf-8") as fh:
        fh.write("")
    cmd = ["cog", "--debug", "predict", docker_image]

    for k, v in inputs.items():
        cmd += ["-i", f"{k}={v}"]

    result = subprocess.run(
        cmd,
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
    )
    assert result.stdout == "hello default 20 world jpg foo 6\n"
    assert "falling back to slow loader" not in str(result.stderr)


def test_predict_path_list_input(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/path-list-input-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    with open(out_dir / "1.txt", "w", encoding="utf-8") as fh:
        fh.write("test1")
    with open(out_dir / "2.txt", "w", encoding="utf-8") as fh:
        fh.write("test2")
    cmd = ["cog", "predict", "-i", "paths=@1.txt", "-i", "paths=@2.txt"]

    result = subprocess.run(
        cmd,
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert "test1" in result.stdout
    assert "test2" in result.stdout


def test_predict_works_with_deferred_annotations():
    project_dir = Path(__file__).parent / "fixtures/future-annotations-project"

    subprocess.check_call(
        ["cog", "predict", "-i", "input=world"],
        cwd=project_dir,
        timeout=DEFAULT_TIMEOUT,
    )


def test_predict_int_none_output():
    project_dir = Path(__file__).parent / "fixtures/int-none-output-project"

    subprocess.check_call(
        ["cog", "predict"],
        cwd=project_dir,
        timeout=DEFAULT_TIMEOUT,
    )


def test_predict_string_none_output():
    project_dir = Path(__file__).parent / "fixtures/string-none-output-project"

    subprocess.check_call(
        ["cog", "predict"],
        cwd=project_dir,
        timeout=DEFAULT_TIMEOUT,
    )
