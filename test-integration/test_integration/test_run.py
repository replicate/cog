import subprocess
from pathlib import Path

import pexpect
import pytest


def test_run(tmpdir_factory, cog_binary):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.8"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        [cog_binary, "run", "echo", "hello world"],
        cwd=tmpdir,
        check=True,
        capture_output=True,
    )
    assert b"hello world" in result.stdout


def test_run_with_secret(tmpdir_factory, cog_binary):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.8"
  run:
    - echo hello world
    - command: >-
        echo shh
      mounts:
        - type: secret
          id: foo
          target: secret.txt
        """
        f.write(cog_yaml)
    with open(tmpdir / "secret.txt", "w") as f:
        f.write("🤫")

    result = subprocess.run(
        [cog_binary, "debug"],
        cwd=tmpdir,
        check=True,
        capture_output=True,
    )
    assert b"RUN echo hello world" in result.stdout
    assert b"RUN --mount=type=secret,id=foo,target=secret.txt echo shh" in result.stdout


def test_run_fast_build(cog_binary):
    project_dir = Path(__file__).parent / "fixtures/fast-build"
    result = subprocess.run(
        [cog_binary, "run", "echo", "hello world"],
        cwd=project_dir,
        check=True,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0
    assert result.stdout == "hello world\n"


def test_run_with_unconsumed_piped_stdin(tmpdir_factory, cog_binary):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.13"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        [cog_binary, "run", "echo", "hello-from-echo"],
        cwd=tmpdir,
        check=True,
        capture_output=True,
        input=b"hello-from-stdin\n",
    )
    assert result.returncode == 0
    assert b"hello-from-echo" in result.stdout


def test_run_with_unattached_stdin(tmpdir_factory, cog_binary):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.13"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        [cog_binary, "run", "echo", "hello-from-echo"],
        cwd=tmpdir,
        check=True,
        capture_output=True,
    )
    assert result.returncode == 0
    assert b"hello-from-echo" in result.stdout


def test_run_with_piped_stdin_returned_to_stdout(tmpdir_factory, cog_binary):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.13"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        [cog_binary, "run", "cat"],
        cwd=tmpdir,
        check=True,
        capture_output=True,
        input=b"hello world\n",
    )
    assert result.returncode == 0
    assert b"hello world" in result.stdout.splitlines()


@pytest.mark.skipif(
    pexpect is None,
    reason="pexpect not available; install it in integration env",
)
def test_run_shell_with_with_interactive_tty(tmpdir_factory, cog_binary):
    tmpdir = tmpdir_factory.mktemp("project")
    (tmpdir / "cog.yaml").write_text(
        "build:\n  python_version: '3.13'\n  cog_runtime: true\n",
        encoding="utf-8",
    )

    child = pexpect.spawn(
        str(cog_binary) + " run /bin/bash",
        cwd=str(tmpdir),
        encoding="utf-8",
        timeout=20,
    )
    child.expect(r"#")  # wait for bash prompt
    child.sendline("echo OK")
    child.expect("OK")
    child.sendline("exit")
    child.close()
