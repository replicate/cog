import json
import os
import subprocess
from pathlib import Path

import pytest


def test_build_without_predictor(docker_image):
    project_dir = Path(__file__).parent / "fixtures/no-predictor-project"
    build_process = subprocess.run(
        ["cog", "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode > 0
    assert (
        "Can't run predictions: 'predict' option not found"
        in build_process.stderr.decode()
    )


def test_build_names_uses_image_option_in_cog_yaml(tmpdir, docker_image):
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = f"""
image: {docker_image}
build:
  python_version: 3.8
predict: predict.py:Predictor
"""
        f.write(cog_yaml)

    with open(tmpdir / "predict.py", "w") as f:
        code = """
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text

"""
        f.write(code)

    subprocess.run(
        ["cog", "build"],
        cwd=tmpdir,
        check=True,
    )
    assert docker_image in str(
        subprocess.run(["docker", "images"], capture_output=True, check=True).stdout
    )


def test_build_with_model(docker_image):
    project_dir = Path(__file__).parent / "fixtures/path-project"
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
    schema = json.loads(labels["run.cog.openapi_schema"])

    assert schema["components"]["schemas"]["Input"] == {
        "title": "Input",
        "required": ["text", "path"],
        "type": "object",
        "properties": {
            "text": {"title": "Text", "type": "string", "x-order": 0},
            "path": {
                "title": "Path",
                "type": "string",
                "format": "uri",
                "x-order": 1,
            },
        },
    }


def test_build_invalid_schema(docker_image):
    project_dir = Path(__file__).parent / "fixtures/invalid-int-project"
    build_process = subprocess.run(
        ["cog", "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode > 0
    assert "invalid default: number must be at least 2" in build_process.stderr.decode()


@pytest.mark.skipif(os.environ.get("CI") != "true", reason="only runs in CI")
def test_build_gpu_model_on_cpu(tmpdir, docker_image):
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
build:
  python_version: 3.8
  gpu: true
predict: predict.py:Predictor
"""
        f.write(cog_yaml)

    with open(tmpdir / "predict.py", "w") as f:
        code = """
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text

"""
        f.write(code)

    subprocess.run(
        ["git", "config", "--global", "user.email", "noreply@replicate.com"],
        cwd=tmpdir,
        check=True,
    )

    subprocess.run(
        ["git", "config", "--global", "user.name", "Replicate Test Bot"],
        cwd=tmpdir,
        check=True,
    )

    subprocess.run(
        ["git", "init"],
        cwd=tmpdir,
        check=True,
    )
    subprocess.run(
        ["git", "commit", "--allow-empty", "-m", "initial"],
        cwd=tmpdir,
        check=True,
    )
    subprocess.run(
        ["git", "tag", "0.0.1"],
        cwd=tmpdir,
        check=True,
    )

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

    assert len(labels["run.cog.version"]) > 0
    assert json.loads(labels["run.cog.config"]) == {
        "build": {
            "python_version": "3.8",
            "gpu": True,
            "cuda": "11.8",
            "cudnn": "8",
        },
        "predict": "predict.py:Predictor",
    }
    assert "run.cog.openapi_schema" in labels

    assert len(labels["org.opencontainers.image.version"]) > 0
    assert len(labels["org.opencontainers.image.revision"]) > 0


def test_build_with_cog_init_templates(tmpdir, docker_image):
    subprocess.run(
        ["cog", "init"],
        cwd=tmpdir,
        capture_output=True,
        check=True,
    )

    build_process = subprocess.run(
        ["cog", "build", "-t", docker_image],
        cwd=tmpdir,
        capture_output=True,
        check=True,
    )

    assert build_process.returncode == 0
    assert "Image built as cog-" in build_process.stderr.decode()


def test_build_with_complex_output(tmpdir, docker_image):
    project_dir = Path(__file__).parent / "fixtures/complex_output_project"
    build_process = subprocess.run(
        ["cog", "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    assert "Image built as cog-" in build_process.stderr.decode()


def test_python_37_deprecated(docker_image):
    project_dir = Path(__file__).parent / "fixtures/python_37"
    build_process = subprocess.run(
        ["cog", "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode > 0
    assert (
        "minimum supported Python version is 3.8. requested 3.7"
        in build_process.stderr.decode()
    )
