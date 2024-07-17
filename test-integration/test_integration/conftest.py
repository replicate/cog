import os

import pytest

from .util import random_string, remove_docker_image


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
    remove_docker_image(docker_image_name)
