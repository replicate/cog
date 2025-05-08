import os
import subprocess


def test_config(tmpdir_factory, cog_binary):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: "3.8"
        """
        f.write(cog_yaml)

    subdir = tmpdir / "some/sub/dir"
    os.makedirs(subdir)

    result = subprocess.run(
        [cog_binary, "run", "echo", "hello world"],
        cwd=subdir,
        check=True,
        capture_output=True,
    )
    assert b"hello world" in result.stdout
