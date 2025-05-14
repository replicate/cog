import asyncio
import os
import pathlib
import shutil
import subprocess
import time
from pathlib import Path

import httpx
import pytest

from .util import cog_server_http_run

DEFAULT_TIMEOUT = 60


def test_predict_takes_string_inputs_and_returns_strings_to_stdout(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "s=world"],
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


def test_predict_supports_async_predictors(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/async-string-project"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "s=world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == "hello world\n"


def test_predict_takes_int_inputs_and_returns_ints_to_stdout(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/int-project"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "num=2"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == "4\n"
    assert "falling back to slow loader" not in result.stderr


def test_predict_takes_file_inputs(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/path-input-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    with open(out_dir / "input.txt", "w", encoding="utf-8") as fh:
        fh.write("what up")
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "path=@" + str(out_dir / "input.txt")],
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.stdout == "what up\n"
    assert "falling back to slow loader" not in result.stderr


def test_predict_writes_files_to_files(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/path-output-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        [cog_binary, "predict", "--debug"],
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


def test_predict_writes_files_to_files_with_custom_name(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/path-output-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-o", out_dir / "myoutput.bmp"],
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


def test_predict_writes_multiple_files_to_files(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/path-list-output-project"

    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)

    result = subprocess.run(
        [cog_binary, "predict"],
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


def test_predict_writes_strings_to_files(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "s=world", "-o", out_dir / "out.txt"],
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


def test_predict_runs_an_existing_image(docker_image, tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"

    subprocess.run(
        [cog_binary, "build", "--debug", "-t", docker_image],
        cwd=project_dir,
        check=True,
    )

    # Run in another directory to ensure it doesn't use cog.yaml
    another_directory = tmpdir_factory.mktemp("project")
    result = subprocess.run(
        [cog_binary, "predict", "--debug", docker_image, "-i", "s=world"],
        cwd=another_directory,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    assert result.stdout == "hello world\n"
    assert "cannot use fast loader as current Python <3.9" in result.stderr
    assert "falling back to slow loader" in result.stderr


# https://github.com/replicate/cog/commit/28202b12ea40f71d791e840b97a51164e7be3b3c
# we need to find a better way to test this
@pytest.mark.skip("incredibly slow")
def test_predict_with_remote_image(tmpdir_factory, cog_binary):
    image_name = "r8.im/replicate/hello-world@sha256:5c7d5dc6dd8bf75c1acaa8565735e7986bc5b66206b55cca93cb72c9bf15ccaa"
    subprocess.run(["docker", "rmi", "-f", image_name], check=True)

    # Run in another directory to ensure it doesn't use cog.yaml
    another_directory = tmpdir_factory.mktemp("project")
    result = subprocess.run(
        [cog_binary, "predict", image_name, "-i", "text=world"],
        cwd=another_directory,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )

    # lots of docker pull logs are written to stdout before writing the actual output
    # TODO: clean up docker output so cog predict is always clean
    assert result.stdout.strip().endswith("hello world")


def test_predict_in_subdirectory_with_imports(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/subdirectory-project"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "s=world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.stdout == "hello world\n"
    assert "falling back to slow loader" not in result.stderr


def test_predict_many_inputs(tmpdir_factory, cog_binary):
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
    cmd = [cog_binary, "--debug", "predict"]

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


def test_predict_many_inputs_with_existing_image(
    docker_image, tmpdir_factory, cog_binary
):
    project_dir = Path(__file__).parent / "fixtures/many-inputs-project"

    subprocess.run(
        [cog_binary, "build", "--debug", "-t", docker_image],
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
    cmd = [cog_binary, "--debug", "predict", docker_image]

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


def test_predict_path_list_input(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/path-list-input-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    with open(out_dir / "1.txt", "w", encoding="utf-8") as fh:
        fh.write("test1")
    with open(out_dir / "2.txt", "w", encoding="utf-8") as fh:
        fh.write("test2")
    cmd = [cog_binary, "predict", "-i", "paths=@1.txt", "-i", "paths=@2.txt"]

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


@pytest.mark.parametrize(
    ("fixture_name",),
    [
        ("simple",),
        ("double-fork",),
        ("double-fork-http",),
        ("multiprocessing",),
    ],
)
def test_predict_with_subprocess_in_setup(fixture_name, cog_binary):
    project_dir = (
        Path(__file__).parent / "fixtures" / f"setup-subprocess-{fixture_name}-project"
    )

    with cog_server_http_run(project_dir, cog_binary) as addr:
        busy_count = 0

        for i in range(100):
            response = httpx.post(
                f"{addr}/predictions",
                json={"input": {"s": f"friendo{i}"}},
            )
            if response.status_code == 409:
                busy_count += 1
                continue

            assert response.status_code == 200, str(response)

        assert busy_count < 10


@pytest.mark.asyncio
async def test_concurrent_predictions(cog_binary):
    async def make_request(i: int) -> httpx.Response:
        return await client.post(
            f"{addr}/predictions",
            json={
                "id": f"id-{i}",
                "input": {"s": f"sleepyhead{i}", "sleep": 1.0},
            },
        )

    with cog_server_http_run(
        Path(__file__).parent / "fixtures" / "async-sleep-project", cog_binary
    ) as addr:
        async with httpx.AsyncClient() as client:
            tasks = []
            start = time.perf_counter()
            async with asyncio.TaskGroup() as tg:
                for i in range(5):
                    tasks.append(tg.create_task(make_request(i)))
                # give time for all of the predictions to be accepted, but not completed
                await asyncio.sleep(0.2)
                # we shut the server down, but expect all running predictions to complete
                await client.post(f"{addr}/shutdown")
            end = time.perf_counter()
            assert (end - start) < 3.0  # ensure the predictions ran concurrently
            for i, task in enumerate(tasks):
                assert task.result().status_code == 200
                assert task.result().json()["output"] == f"wake up sleepyhead{i}"


def test_predict_new_union_project(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/new-union-project"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "text=world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.returncode == 0
    assert result.stdout == "hello world\n"


def test_predict_with_fast_build_with_local_image(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/fast-build"
    weights_file = os.path.join(project_dir, "weights.h5")
    with open(weights_file, "w", encoding="utf8") as handle:
        handle.seek(256 * 1024 * 1024)
        handle.write("\0")

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image, "--x-fast", "--x-localimage"],
        cwd=project_dir,
        capture_output=True,
    )

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            docker_image,
            "--x-fast",
            "--x-localimage",
            "--debug",
            "-i",
            "s=world",
        ],
        cwd=project_dir,
        capture_output=True,
    )
    os.remove(weights_file)
    assert build_process.returncode == 0
    assert result.returncode == 0


def test_predict_optional_project(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/optional-project"
    result = subprocess.run(
        [cog_binary, "predict", "--debug"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    # stdout should be clean without any log messages so it can be piped to other commands
    assert result.returncode == 0
    assert result.stdout == "hello No One\n"


def test_predict_complex_types(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/complex-types"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image, "--x-fast", "--x-localimage"],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    result = subprocess.run(
        [
            cog_binary,
            "predict",
            "--debug",
            docker_image,
            "-i",
            'message={"content": "Hi There", "role": "user"}',
        ],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    assert result.stdout == "Content: Hi There\n"


def test_predict_overrides_project(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/overrides-project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    result = subprocess.run(
        [cog_binary, "predict", "--debug", docker_image],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    assert result.stdout == "hello 1.26.4\n"


def test_predict_zsh_package(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/zsh-package"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    result = subprocess.run(
        [cog_binary, "predict", "--debug", docker_image],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    assert ",sh," in result.stdout
    assert ",zsh," in result.stdout


def test_predict_string_list(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-list-project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    result = subprocess.run(
        [cog_binary, "predict", "--debug", docker_image, "-i", "s=world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    assert result.stdout == "hello world\n"


def test_predict_granite_project(docker_image, cog_binary):
    # We are checking that we are not clobbering pydantic to a <2 version.
    project_dir = Path(__file__).parent / "fixtures/granite-project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    result = subprocess.run(
        [cog_binary, "predict", "--debug", docker_image],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    assert result.stdout == "2.11.3\n"


def test_predict_fast_build(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/fast-build"

    result = subprocess.run(
        [cog_binary, "predict", "--x-fast", "-i", "s=world"],
        cwd=project_dir,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0
    assert result.stdout == "hello world\n"


def test_predict_env_vars(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/env-project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    result = subprocess.run(
        [cog_binary, "predict", "--debug", docker_image, "-i", "name=TEST_VAR"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    assert result.stdout == "ENV[TEST_VAR]=test_value\n"


def test_predict_complex_types_list(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/complex-types-list-project"

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            "--debug",
            "-i",
            'messages=[{"content": "Hi There", "role": "user"}, {"content": "I am a test", "role": "user"}]',
        ],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    assert result.stdout == "Content: Hi There-I am a test\n"


def test_predict_tensorflow_project(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/tensorflow-project"

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            "--debug",
        ],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    assert result.stdout == "2.10.0\n"
