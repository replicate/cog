import os
import subprocess

import pytest

from .util import random_string


def pytest_sessionstart(session):
    os.environ["COG_NO_UPDATE_CHECK"] = "1"


@pytest.fixture
def docker_image_name():
    return "cog-test-" + random_string(10)


@pytest.fixture
def docker_image(docker_image_name):
    yield docker_image_name
    # We expect the image to exist by this point and will fail if it doesn't.
    # If you just need a name, use docker_image_name.
    subprocess.run(
        ["docker", "rmi", docker_image_name], check=True, capture_output=True
    )
