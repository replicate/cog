import json
from pathlib import Path
import subprocess
from .util import random_string


def test_build_without_predictor(docker_image):
    project_dir = Path(__file__).parent / "fixtures/no-predictor-project"
    subprocess.run(
        ["cog", "build", "-t", docker_image],
        cwd=project_dir,
        check=True,
    )
    assert docker_image in str(
        subprocess.run(["docker", "images"], capture_output=True, check=True).stdout
    )
    image = json.loads(
        subprocess.run(
            ["docker", "image", "inspect", docker_image],
            capture_output=True,
            check=True,
        ).stdout
    )
    labels = image[0]["Config"]["Labels"]
    assert len(labels["org.cogmodel.cog_version"]) > 0
    assert json.loads(labels["org.cogmodel.config"]) == {
        "build": {"python_version": "3.8"}
    }
    assert json.loads(labels["org.cogmodel.type_signature"]) == {}


def test_build_names_uses_image_option_in_cog_yaml(tmpdir_factory, docker_image):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = f"""
image: {docker_image}
build:
  python_version: 3.8
"""
        f.write(cog_yaml)

    subprocess.run(
        ["cog", "build"],
        cwd=tmpdir,
        check=True,
    )
    assert docker_image in str(
        subprocess.run(["docker", "images"], capture_output=True, check=True).stdout
    )


def test_build_with_model(docker_image):
    project_dir = Path(__file__).parent / "fixtures/file-project"
    subprocess.run(
        ["cog", "build", "-t", docker_image],
        cwd=project_dir,
        check=True,
    )
    image = json.loads(
        subprocess.run(
            ["docker", "image", "inspect", docker_image],
            capture_output=True,
            check=True,
        ).stdout
    )
    labels = image[0]["Config"]["Labels"]
    assert json.loads(labels["org.cogmodel.type_signature"]) == {
        "inputs": [
            {"name": "text", "type": "str"},
            {"name": "path", "type": "Path"},
        ]
    }


def test_build_gpu_model_on_cpu(tmpdir_factory, docker_image):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: 3.8
  gpu: true
"""
        f.write(cog_yaml)

    subprocess.run(
        ["cog", "build", "-t", docker_image],
        cwd=tmpdir,
        check=True,
    )
    assert docker_image in str(
        subprocess.run(["docker", "images"], capture_output=True, check=True).stdout
    )
    image = json.loads(
        subprocess.run(
            ["docker", "image", "inspect", docker_image],
            capture_output=True,
            check=True,
        ).stdout
    )
    labels = image[0]["Config"]["Labels"]
    assert len(labels["org.cogmodel.cog_version"]) > 0
    assert json.loads(labels["org.cogmodel.config"]) == {
        "build": {
            "python_version": "3.8",
            "gpu": True,
            "cuda": "11.2",
            "cudnn": "8",
        }
    }
    assert json.loads(labels["org.cogmodel.type_signature"]) == {}
