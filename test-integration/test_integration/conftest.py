import subprocess
import pytest

from .util import random_string, find_free_port, docker_run, get_local_ip, wait_for_port


@pytest.fixture
def redis_port():
    container_name = "cog-test-redis-" + random_string(10)
    port = find_free_port()
    with docker_run(
        "redis",
        name=container_name,
        publish=[{"host": port, "container": 6379}],
        detach=True,
    ):
        wait_for_port(get_local_ip(), port)
        yield port


@pytest.fixture
def docker_image():
    image = "cog-test-" + random_string(10)
    yield image
    subprocess.run(["docker", "rmi", "-f", image], check=False)
