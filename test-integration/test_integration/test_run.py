import subprocess


def test_run(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
environment:
  python: "3.8"
        """
        f.write(cog_yaml)

    result = subprocess.run(
        ["cog", "run", "echo", "hello world"],
        cwd=tmpdir,
        check=True,
        capture_output=True,
    )
    assert b"hello world" in result.stdout
