import subprocess


def test_run(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.8"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        ["cog", "run", "echo", "hello world"],
        cwd=tmpdir,
        check=True,
        capture_output=True,
    )
    assert b"hello world" in result.stdout


def test_run_with_secret(tmpdir_factory):
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
        ["cog", "debug"],
        cwd=tmpdir,
        check=True,
        capture_output=True,
    )
    assert b"RUN echo hello world" in result.stdout
    assert b"RUN --mount=type=secret,id=foo,target=secret.txt echo shh" in result.stdout
