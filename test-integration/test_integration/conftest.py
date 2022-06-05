import os
from pathlib import Path
import subprocess
import pytest

import redis

from .util import random_string, find_free_port, docker_run, wait_for_port


def pytest_sessionstart(session):
    os.environ["COG_NO_UPDATE_CHECK"] = "1"


@pytest.fixture
def docker_network():
    name = "cog-test-" + random_string(10)
    subprocess.run(["docker", "network", "create", name])
    yield name
    subprocess.run(["docker", "network", "rm", name])


@pytest.fixture
def redis_port(docker_network):
    """Start a redis server inside the Docker network.
    Inside the network, it is available at redis:6379
    Outside the network, it is available at localhost:redis_port
    """
    port = find_free_port()
    with docker_run(
        "redis",
        net_alias="redis",
        network=docker_network,
        publish=[{"host": port, "container": 6379}],
        detach=True,
    ):
        wait_for_port("localhost", port)
        yield port


@pytest.fixture
def redis_client(redis_port):
    yield redis.Redis("localhost", redis_port)


@pytest.fixture
def docker_image():
    image = "cog-test-" + random_string(10)
    yield image
    subprocess.run(["docker", "rmi", "-f", image], check=False)


@pytest.fixture
def upload_server_image():
    """
    Build the upload server once for the test run. The image doesn't change.
    """
    subprocess.run(
        ["docker", "build", "-t", "cog-test-upload-server", "."],
        cwd=Path(__file__).parent.parent / "upload_server",
        check=True,
    )
    return "cog-test-upload-server"


@pytest.fixture
def upload_server(docker_network, upload_server_image, tmpdir_factory):
    """
    Run a server that can be used to upload and download files from.

    It is accessible at http://upload-server:5000 inside the network. The thing returned is the path for uploads.
    """
    tmpdir = tmpdir_factory.mktemp("uploads")
    with docker_run(
        upload_server_image,
        net_alias="upload-server",
        network=docker_network,
        volumes=["-v", f"{tmpdir}:/uploads"],
        detach=True,
    ):
        yield tmpdir
