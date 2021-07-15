import os
import subprocess
from typing import List

from .util import random_string, set_model_url, push_with_log


def test_docker_cleanup(cog_server, project_dir):
    user = random_string(10)
    model_name = random_string(10)
    model_url = f"http://localhost:{cog_server.port}/{user}/{model_name}"

    set_model_url(model_url, project_dir)

    push_with_log(project_dir)

    # check that only the :latest version remains locally
    tags = list_docker_tags(cog_server.registry_host, model_name)
    assert tags == ["latest"]

    # push a new version
    with open(project_dir / "newfile.txt", "w") as f:
        f.write("i'm new here")

    push_with_log(project_dir)

    # check that only the :latest version remains locally, still
    tags = list_docker_tags(cog_server.registry_host, model_name)
    assert tags == ["latest"]


def list_docker_tags(registry_host, model_name) -> List[str]:
    return (
        subprocess.Popen(
            [
                "docker",
                "images",
                f"{registry_host}/{model_name}",
                "--format",
                "{{.Tag}}",
            ],
            stdout=subprocess.PIPE,
        )
        .communicate()[0]
        .decode()
        .strip()
        .splitlines()
    )
