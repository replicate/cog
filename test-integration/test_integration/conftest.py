import os
from pathlib import Path
import subprocess

import pytest

from .util import random_string


def pytest_sessionstart(session):
    os.environ["COG_NO_UPDATE_CHECK"] = "1"


@pytest.fixture
def docker_image():
    image = "cog-test-" + random_string(10)
    yield image
    subprocess.run(["docker", "rmi", "-f", image], check=False)
