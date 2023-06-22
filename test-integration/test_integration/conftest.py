import os
import subprocess

import pytest

from .util import random_string


def pytest_sessionstart(session):
    os.environ["COG_NO_UPDATE_CHECK"] = "1"

def pytest_addoption(parser):
    parser.addoption("--cog-path", action="store", default=None, help="Path to Cog executable")

@pytest.fixture
def cog_path(request):
    return request.config.getoption("--cog-path")

@pytest.fixture
def docker_image():
    image = "cog-test-" + random_string(10)
    yield image
    subprocess.run(["docker", "rmi", "-f", image], check=False)
