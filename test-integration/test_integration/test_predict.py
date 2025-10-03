import asyncio
import os
import pathlib
import shutil
import subprocess
import time
from pathlib import Path

import httpx
import pytest
from pytest_httpserver import HTTPServer
from werkzeug import Request, Response

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

    assert result.returncode == 0
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


def test_predict_with_fast_build_with_local_image(fixture, docker_image, cog_binary):
    project_dir = fixture("fast-build")
    weights_file = os.path.join(project_dir, "weights.h5")
    with open(weights_file, "w", encoding="utf8") as handle:
        handle.seek(256 * 1024 * 1024)
        handle.write("\0")

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image, "--x-localimage"],
        cwd=project_dir,
        capture_output=True,
    )

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            docker_image,
            "--x-localimage",
            "--debug",
            "-i",
            "s=world",
        ],
        cwd=project_dir,
        capture_output=True,
    )

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
    assert result.stdout == "2.11.9\n"


def test_predict_fast_build(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/fast-build"

    result = subprocess.run(
        [cog_binary, "predict", "-i", "s=world"],
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
    assert result.stdout == "2.11.1\n"


def test_predict_json_input(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            "--debug",
            "--json",
            '{"s": "sackfield"}',
        ],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    assert (
        result.stdout
        == """{
  "status": "succeeded",
  "output": "hello sackfield",
  "error": ""
}
"""
    )


def test_predict_json_input_filename(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            "--debug",
            "--json",
            "@input.json",
        ],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    assert (
        result.stdout
        == """{
  "status": "succeeded",
  "output": "hello sackfield",
  "error": ""
}
"""
    )


def test_predict_json_input_stdin(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            "--debug",
            "--json",
            "@-",
        ],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=120.0,
        input='{"s": "sackfield"}',
    )
    assert result.returncode == 0
    assert (
        result.stdout
        == """{
  "status": "succeeded",
  "output": "hello sackfield",
  "error": ""
}
"""
    )


def test_predict_json_output(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            "--debug",
            "--json",
            '{"s": "sackfield"}',
            "--output",
            "output.json",
        ],
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    with open(out_dir / "output.json", encoding="utf-8") as f:
        assert (
            f.read()
            == """{
  "status": "succeeded",
  "output": "hello sackfield",
  "error": ""
}"""
        )


def test_predict_json_input_stdin_dash(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"

    result = subprocess.run(
        [
            cog_binary,
            "predict",
            "--debug",
            "--json",
            "-",
        ],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=120.0,
        input='{"s": "sackfield"}',
    )
    assert result.returncode == 0
    assert (
        result.stdout
        == """{
  "status": "succeeded",
  "output": "hello sackfield",
  "error": ""
}
"""
    )


def test_predict_glb_file(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/glb-project"

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


def test_predict_future_annotations(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/future-annotations-project"

    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "image=@some_image.jpg"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0


def test_predict_pipeline(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/procedure-project"
    result = subprocess.run(
        [cog_binary, "predict", "--x-pipeline", "--debug", "-i", "prompt=test"],
        cwd=project_dir,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    assert result.stdout == "HELLO TEST\n"


def test_predict_cog_runtime_float(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/cog-runtime-float"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "num=10"],
        cwd=project_dir,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    assert result.stdout == "20\n"


def test_predict_cog_runtime_float_negative(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/cog-runtime-float"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "num=-10"],
        cwd=project_dir,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    assert result.stdout == "-20\n"


def test_predict_cog_runtime_int(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/cog-runtime-int"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "num=10"],
        cwd=project_dir,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    assert result.stdout == "20\n"


def test_predict_cog_runtime_int_negative(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/cog-runtime-int"
    result = subprocess.run(
        [cog_binary, "predict", "--debug", "-i", "num=-10"],
        cwd=project_dir,
        capture_output=True,
        text=True,
        timeout=120.0,
    )
    assert result.returncode == 0
    assert result.stdout == "-20\n"


def test_predict_pipeline_downloaded_requirements(cog_binary):
    """Test that pipeline builds download runtime requirements and make dependencies available"""
    project_dir = Path(__file__).parent / "fixtures/pipeline-requirements-project"

    # Create initial local requirements.txt that differs from what mock server will return
    # This simulates the out-of-sync scenario
    # TODO[md]: remove this stuff once tests are ported to go and each test
    # has an isolated temp dir so we don't need to care about temp files polluting the work tree
    initial_local_requirements = """# pipelines-runtime@sha256:d1b9fbd673288453fdf12806f4cba9e9e454f0f89b187eac2db5731792f71d60
moviepy==v2.2.1
numpy==v2.3.2
pillow==v11.3.0
pydantic==v1.10.22
replicate==v2.0.0a22
requests==v2.32.5
scikit-learn==v1.7.1
"""

    # Create a mock requirements file that includes basic packages needed for validation
    mock_requirements = """# Mock runtime requirements for testing
requests==2.32.5
urllib3==2.0.4
"""

    # Write the initial local requirements file (will be overwritten during test)
    requirements_file = project_dir / "requirements.txt"
    requirements_file.write_text(initial_local_requirements)

    try:
        # Set up a mock HTTP server to serve the requirements file
        with HTTPServer(host="127.0.0.1", port=0) as httpserver:

            def requirements_handler(request: Request) -> Response:
                if request.path == "/requirements.txt":
                    # Include ETag header as expected by the requirements download logic
                    headers = {"ETag": '"mock-requirements-etag-123"'}
                    return Response(
                        mock_requirements,
                        status=200,
                        headers=headers,
                        content_type="text/plain",
                    )
                return Response("Not Found", status=404)

            httpserver.expect_request("/requirements.txt").respond_with_handler(
                requirements_handler
            )

            # Get the server URL (context manager already started the server)
            server_host = f"127.0.0.1:{httpserver.port}"

            # Run prediction with pipeline flag and mock server
            env = os.environ.copy()
            env["R8_PIPELINES_RUNTIME_HOST"] = server_host
            env["R8_SCHEME"] = "http"  # Use HTTP instead of HTTPS for testing

            result = subprocess.run(
                [cog_binary, "predict", "--x-pipeline", "--debug"],
                cwd=project_dir,
                capture_output=True,
                text=True,
                timeout=120.0,
                env=env,
            )

            # Should succeed since packages should be available from downloaded requirements
            assert result.returncode == 0

            # The output should list all installed packages (one per line)
            # No header since predict function was simplified to just return package list
            assert (
                len(result.stdout.strip().split("\n")) > 10
            )  # Should have many packages

            # Should not contain error messages
            assert "ERROR:" not in result.stdout

            # Verify that the mock requirements were downloaded by checking debug output
            assert "Generated requirements.txt:" in result.stderr
            # Should show requests from our mock requirements (proves download worked)
            assert "requests==2.32.5" in result.stderr

            # Verify that specific versions from mock are present in the installed packages list
            # This proves the mock requirements were actually installed and used
            assert "requests==2.32.5" in result.stdout
            assert (
                "urllib3==" in result.stdout
            )  # Just check that urllib3 is present with some version

            # Verify that the local requirements.txt file was updated with mock requirements
            local_requirements_path = project_dir / "requirements.txt"
            with open(local_requirements_path, "r") as f:
                local_requirements_content = f.read()
            # Should contain our mock requirements
            assert "requests==2.32.5" in local_requirements_content
            assert (
                "# Mock runtime requirements for testing" in local_requirements_content
            )

    finally:
        # Clean up: remove the dynamic requirements.txt file so it doesn't affect git
        if requirements_file.exists():
            requirements_file.unlink()
