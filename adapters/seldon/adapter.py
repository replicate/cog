#!/usr/bin/env python

import sys
import subprocess
import os


def main():
    model_python_module = os.environ["COG_MODEL_PYTHON_MODULE"]
    model_class = os.environ["COG_MODEL_CLASS"]
    cpu_image = os.environ["COG_CPU_IMAGE"]
    docker_registry = os.environ["COG_DOCKER_REGISTRY"]
    has_docker_registry = os.environ["COG_HAS_DOCKER_REGISTRY"] == "true"
    user = os.environ["COG_REPO_USER"]
    name = os.environ["COG_REPO_NAME"]
    model_id = os.environ["COG_MODEL_ID"]

    if not has_docker_registry:
        docker_registry = "no_registry"

    seldon_model = f"""
from {model_python_module} import {model_class} as Model

class SeldonModel:

    def __init__(self):
        self.model = Model()
        self.model.setup()

    def predict(self, features, feature_names, **kwargs):
        raw_inputs = dict(zip(feature_names, features))
        cleanup_functions = []
        inputs = self.model.validate_and_convert_inputs(raw_inputs, cleanup_functions)
        try:
            return self.model.run(**inputs)
        finally:
            self.model.run_cleanup_functions(cleanup_functions)
"""
    with open("SeldonModel.py", "w") as f:
        f.write(seldon_model)

    dockerfile = f"""
FROM {cpu_image}

RUN pip install seldon-core
COPY SeldonModel.py .
CMD exec seldon-core-microservice SeldonModel --service-type MODEL --persistence 0
"""
    dockerfile_name = "Dockerfile.seldon"
    with open(dockerfile_name, "w") as f:
        f.write(dockerfile)

    sys.stderr.write("Building Seldon image\n")

    tag = f"{docker_registry}/{user}/{name}:{model_id}-seldon"
    out = subprocess.check_output(
        [
            "docker",
            "build",
            "-f",
            dockerfile_name,
            "--progress=plain",
            "--tag",
            tag,
            ".",
        ]
    ).decode()
    last_line = out.strip().splitlines()[-1]
    if not last_line.startswith("Successfully tagged "):
        raise Exception("Seldon build failed:\n" + out)

    if has_docker_registry:
        sys.stderr.write("Pushing Seldon package\n")
        out = subprocess.check_output(["docker", "push", tag]).decode()
        # TODO: check for errors

    sys.stderr.write(f"Successfully built Seldon container {tag}\n")
    print(tag)


if __name__ == "__main__":
    main()
