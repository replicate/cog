import json
import subprocess
from .util import random_string


def test_build(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: 3.8
"""
        f.write(cog_yaml)

    image_name = "cog-test-" + random_string(length=10)

    try:
        subprocess.run(
            ["cog", "build", "-t", image_name],
            cwd=tmpdir,
            check=True,
        )
        assert image_name in str(
            subprocess.run(["docker", "images"], capture_output=True).stdout
        )
        image = json.loads(
            subprocess.run(
                ["docker", "image", "inspect", image_name], capture_output=True
            ).stdout
        )
        labels = image[0]["Config"]["Labels"]
        assert len(labels["org.cogmodel.cog_version"]) > 0
        print(labels["org.cogmodel.config"])
        assert json.loads(image[0]["Config"]["Labels"]["org.cogmodel.config"]) == {
            "build": {"python_version": "3.8"}
        }
        assert (
            json.loads(image[0]["Config"]["Labels"]["org.cogmodel.type_signature"])
            == {}
        )
    finally:
        subprocess.run(["docker", "rmi", image_name], check=False)


def test_build_image_option(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    image_name = "cog-test-" + random_string(length=10)
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = f"""
image: {image_name}
build:
  python_version: 3.8
"""
        f.write(cog_yaml)

    try:
        subprocess.run(
            ["cog", "build"],
            cwd=tmpdir,
            check=True,
        )
        assert image_name in str(
            subprocess.run(["docker", "images"], capture_output=True).stdout
        )
    finally:
        subprocess.run(["docker", "rmi", image_name], check=False)


def test_build_with_model(project_dir):
    image_name = "cog-test-" + random_string(length=10)
    try:
        subprocess.run(
            ["cog", "build", "-t", image_name],
            cwd=project_dir,
            check=True,
        )
        image = json.loads(
            subprocess.run(
                ["docker", "image", "inspect", image_name], capture_output=True
            ).stdout
        )
        labels = image[0]["Config"]["Labels"]
        assert json.loads(
            image[0]["Config"]["Labels"]["org.cogmodel.type_signature"]
        ) == {
            "inputs": [
                {"name": "text", "type": "str"},
                {"name": "path", "type": "Path"},
                {"name": "output_file", "type": "bool", "default": "False"},
            ]
        }
    finally:
        subprocess.run(["docker", "rmi", image_name], check=False)


def test_build_gpu_model_on_cpu(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: 3.8
  gpu: true
"""
        f.write(cog_yaml)

    image_name = "cog-test-" + random_string(length=10)

    try:
        subprocess.run(
            ["cog", "build", "-t", image_name],
            cwd=tmpdir,
            check=True,
        )
        assert image_name in str(
            subprocess.run(["docker", "images"], capture_output=True).stdout
        )
        image = json.loads(
            subprocess.run(
                ["docker", "image", "inspect", image_name], capture_output=True
            ).stdout
        )
        labels = image[0]["Config"]["Labels"]
        assert len(labels["org.cogmodel.cog_version"]) > 0
        assert json.loads(image[0]["Config"]["Labels"]["org.cogmodel.config"]) == {
            "build": {
                "python_version": "3.8",
                "gpu": True,
                "cuda": "11.2",
                "cudnn": "8",
            }
        }
        assert (
            json.loads(image[0]["Config"]["Labels"]["org.cogmodel.type_signature"])
            == {}
        )
    finally:
        subprocess.run(["docker", "rmi", image_name], check=False)
