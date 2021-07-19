import subprocess
from .util import random_string


def test_build(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
environment:
  python_version: 3.8
"""
        f.write(cog_yaml)

    image = "cog-test-" + random_string(length=10)

    try:
        subprocess.run(
            ["cog", "build", "-t", image],
            cwd=tmpdir,
            check=True,
        )
        assert image in str(
            subprocess.run(["docker", "images"], capture_output=True).stdout
        )
    finally:
        subprocess.run(["docker", "rmi", image], check=False)


def test_build_image_option(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    image = "cog-test-" + random_string(length=10)
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = f"""
image: {image}
environment:
  python_version: 3.8
"""
        f.write(cog_yaml)

    try:
        subprocess.run(
            ["cog", "build"],
            cwd=tmpdir,
            check=True,
        )
        assert image in str(
            subprocess.run(["docker", "images"], capture_output=True).stdout
        )
    finally:
        subprocess.run(["docker", "rmi", image], check=False)
