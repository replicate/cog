import json
import os
import subprocess
from pathlib import Path

import pytest

from .util import assert_versions_match


def test_build_without_predictor(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/no-predictor-project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode > 0
    stderr = build_process.stderr.decode()
    # Different runtimes produce different error formats:
    # - cog: escaped quotes (Python exception)
    # - coglet: unescaped quotes (Go error)
    # - coglet-alpha: JSON formatted error with "invalid predict" or "failed to parse predict"
    assert (
        "Can't run predictions: 'predict' option not found" in stderr
        or "Can\\'t run predictions: \\'predict\\' option not found" in stderr
        or "invalid predict" in stderr
        or "failed to parse predict" in stderr
    )


def test_build_names_uses_image_option_in_cog_yaml(tmpdir, docker_image, cog_binary):
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = f"""
image: {docker_image}
build:
  python_version: 3.9
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
        [cog_binary, "build"],
        cwd=tmpdir,
        check=True,
    )
    assert docker_image in str(
        subprocess.run(["docker", "images"], capture_output=True, check=True).stdout
    )


def test_build_with_model(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/path-project"
    subprocess.run(
        [cog_binary, "build", "-t", docker_image],
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


def test_build_invalid_schema(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/invalid-int-project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode > 0
    stderr = build_process.stderr.decode()
    # Different runtimes produce different error formats:
    # - cog: "invalid default: number must be at least 2"
    # - coglet-alpha: "AssertionError: default=1 conflicts with ge=2"
    assert "invalid default: number must be at least 2" in stderr or (
        "default=1 conflicts with ge=2" in stderr
    )


@pytest.mark.skipif(os.environ.get("CI") != "true", reason="only runs in CI")
def test_build_gpu_model_on_cpu(tmpdir, docker_image, cog_binary):
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
        [cog_binary, "build", "-t", docker_image],
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


def test_build_with_cog_init_templates(tmpdir, docker_image, cog_binary):
    subprocess.run(
        [cog_binary, "init"],
        cwd=tmpdir,
        capture_output=True,
        check=True,
    )

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=tmpdir,
        capture_output=True,
        check=True,
    )

    assert build_process.returncode == 0
    assert "Image built as cog-" in build_process.stderr.decode()


@pytest.mark.skipif(
    os.environ.get("COG_WHEEL") == "coglet-alpha",
    reason="Pinned coglet-alpha version does not support custom Pydantic output models",
)
@pytest.mark.skipif(
    os.environ.get("COG_WHEEL") == "coglet-alpha",
    reason="Pinned coglet-alpha version does not support custom Pydantic output models",
)
def test_build_with_complex_output(tmpdir, docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/complex_output_project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    assert "Image built as cog-" in build_process.stderr.decode()


def test_python_37_deprecated(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/python_37"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode > 0
    assert (
        "minimum supported Python version is 3.8. requested 3.7"
        in build_process.stderr.decode()
    )


def test_build_base_image_sha(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/path-project"
    subprocess.run(
        [cog_binary, "build", "-t", docker_image, "--use-cog-base-image"],
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
    base_layer_hash = labels["run.cog.cog-base-image-last-layer-sha"]
    layers = image[0]["RootFS"]["Layers"]
    assert base_layer_hash in layers


def test_torch_2_0_3_cu118_base_image(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/torch-cuda-baseimage-project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image, "--use-cog-base-image"],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0


def test_torch_1_13_0_base_image_fallback(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/torch-baseimage-project"
    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image, "--openapi-schema", "openapi.json"],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0


def test_torch_1_13_0_base_image_fail_explicit(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/torch-baseimage-project"
    build_process = subprocess.run(
        [
            cog_binary,
            "build",
            "-t",
            docker_image,
            "--openapi-schema",
            "openapi.json",
            "--use-cog-base-image=false",
        ],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0


def test_precompile(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/torch-baseimage-project"
    build_process = subprocess.run(
        [
            cog_binary,
            "build",
            "-t",
            docker_image,
            "--openapi-schema",
            "openapi.json",
            "--use-cog-base-image=false",
            "--precompile",
        ],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0


def test_cog_install_base_image(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/string-project"
    build_process = subprocess.run(
        [
            cog_binary,
            "build",
            "-t",
            docker_image,
            "--use-cog-base-image=true",
        ],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
    cog_installed_version_process = subprocess.run(
        [
            "docker",
            "run",
            "-t",
            docker_image,
            "python",
            "-c",
            "import cog; print(cog.__version__)",
        ],
        cwd=project_dir,
        capture_output=True,
    )
    assert cog_installed_version_process.returncode == 0
    cog_installed_version = cog_installed_version_process.stdout.decode().strip()
    cog_version_process = subprocess.run(
        [
            cog_binary,
            "--version",
        ],
        cwd=project_dir,
        capture_output=True,
    )
    cog_version = cog_version_process.stdout.decode().strip().split()[2]

    assert_versions_match(
        semver_version=cog_version,
        pep440_version=cog_installed_version,
    )


def test_pip_freeze(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/path-project"
    subprocess.run(
        [cog_binary, "build", "-t", docker_image],
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
    pip_freeze = labels["run.cog.pip_freeze"]

    # Filter out the cog/coglet/coglet-alpha packages and fastapi/starlette
    # Different wheels have different package names
    pip_freeze = "\n".join(
        [
            x
            for x in pip_freeze.split("\n")
            if not x.startswith("cog @")
            and not x.startswith("coglet @")
            and not x.startswith("coglet-alpha @")
            and not x.startswith("fastapi")
            and not x.startswith("starlette")
        ]
    )

    # Different wheels may install different typing_extensions versions depending on
    # their dependency resolution. Cog uses >=4.4.0, coglet uses >=4.4.0, and
    # coglet-alpha also uses >=4.4.0. The actual installed version depends on the
    # resolver and other dependencies. Since we now install pydantic with coglet,
    # cog and coglet both end up with 4.12.2 (already in the base image). The old
    # pinned coglet-alpha version has its own dependency resolution that yields 4.15.0.
    cog_wheel = os.environ.get("COG_WHEEL", "cog")
    if cog_wheel == "coglet-alpha":
        typing_ext_version = "4.15.0"
    else:
        # cog and coglet both end up with 4.12.2
        typing_ext_version = "4.12.2"

    expected_pip_freeze = (
        "anyio==4.4.0\n"
        "attrs==23.2.0\n"
        "certifi==2024.8.30\n"
        "charset-normalizer==3.3.2\n"
        "click==8.1.7\n"
        "exceptiongroup==1.2.2\n"
        "h11==0.14.0\n"
        "httptools==0.6.1\n"
        "idna==3.8\n"
        "pydantic==1.10.18\n"
        "python-dotenv==1.0.1\n"
        "PyYAML==6.0.2\n"
        "requests==2.32.3\n"
        "sniffio==1.3.1\n"
        "structlog==24.4.0\n"
        f"typing_extensions=={typing_ext_version}\n"
        "urllib3==2.2.2\n"
        "uvicorn==0.30.6\n"
        "uvloop==0.20.0\n"
        "watchfiles==0.24.0\n"
        "websockets==13.0.1\n"
    )

    assert pip_freeze == expected_pip_freeze


def test_cog_installs_apt_packages(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/apt-packages"
    build_process = subprocess.run(
        [
            cog_binary,
            "build",
            "-t",
            docker_image,
        ],
        cwd=project_dir,
        capture_output=True,
    )
    # Test that the build completes successfully.
    # If the apt-packages weren't installed the run command would fail.
    assert build_process.returncode == 0


def test_fast_build(fixture, docker_image, cog_binary):
    project_dir = fixture("fast-build")
    weights_file = os.path.join(project_dir, "weights.h5")
    with open(weights_file, "w", encoding="utf8") as handle:
        handle.seek(256 * 1024 * 1024)
        handle.write("\0")

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )

    assert build_process.returncode == 0


def test_pydantic2(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/pydantic2"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )

    assert build_process.returncode == 0


def test_ffmpeg_base_image(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/ffmpeg-package"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )

    assert build_process.returncode == 0


def test_bad_dockerignore(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/bad-dockerignore"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )

    assert build_process.returncode == 1
    assert (
        "The .cog tmp path cannot be ignored by docker in .dockerignore"
        in build_process.stderr.decode()
    )


def test_pydantic1_none(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/pydantic1-none"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )

    assert build_process.returncode == 0


def test_fast_build_with_local_image(fixture, docker_image, cog_binary):
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

    assert build_process.returncode == 0


def test_local_whl_install(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/local-whl-install"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )

    assert build_process.returncode == 0


def test_overrides(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/overrides-project"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )

    assert build_process.returncode == 0


def test_install_requires_packaging(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/install-requires-packaging"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image],
        cwd=project_dir,
        capture_output=True,
    )
    print(build_process.stderr.decode())
    assert build_process.returncode == 0


def test_secrets(tmpdir_factory, docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/secrets-project"

    build_process = subprocess.run(
        [
            cog_binary,
            "build",
            "-t",
            docker_image,
            "--secret",
            "id=file-secret,src=file-secret.txt",
            "--secret",
            "id=env-secret,env=ENV_SECRET",
        ],
        cwd=project_dir,
        capture_output=True,
        env={**os.environ, "ENV_SECRET": "env_secret_value"},
    )
    assert build_process.returncode == 0


def test_model_dependencies(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/pipeline-project"
    subprocess.run(
        [cog_binary, "build", "-t", docker_image],
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
    model_dependencies = labels["run.cog.r8_model_dependencies"]
    assert model_dependencies == '["pipelines-beta/upcase"]'


def test_torch_270_cuda_126_base_image(tmpdir_factory, docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/torch-270-cuda-126"

    build_process = subprocess.run(
        [
            cog_binary,
            "build",
            "-t",
            docker_image,
        ],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0


def test_python_313(tmpdir_factory, docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/python-313"

    build_process = subprocess.run(
        [
            cog_binary,
            "build",
            "-t",
            docker_image,
        ],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0


def test_torch_271_cuda_128_base_image(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/torch-271-cuda-128"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image, "--use-cog-base-image"],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0


def test_python_313_base_images(docker_image, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/python-313"

    build_process = subprocess.run(
        [cog_binary, "build", "-t", docker_image, "--use-cog-base-image"],
        cwd=project_dir,
        capture_output=True,
    )
    assert build_process.returncode == 0
