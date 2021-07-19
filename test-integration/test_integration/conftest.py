from typing import Generator
from dataclasses import dataclass
import os
import subprocess
import tempfile
from waiting import wait
import requests
import pytest

from .util import random_string, find_free_port, docker_run, get_local_ip, wait_for_port


@dataclass
class CogServer:
    port: int
    registry_host: str


@pytest.fixture
def registry_host():
    container_name = "cog-test-registry-" + random_string(10)
    port = find_free_port()
    with docker_run(
        "registry:2",
        name=container_name,
        publish=[{"host": port, "container": 5000}],
        detach=True,
    ):
        wait_for_port("localhost", port)
        yield f"localhost:{port}"


@pytest.fixture
def cog_server(registry_host) -> Generator[CogServer, None, None]:
    old_cwd = os.getcwd()
    with tempfile.TemporaryDirectory() as cog_dir:
        os.chdir(cog_dir)
        port = find_free_port()
        server_proc = subprocess.Popen(
            ["cog", "server", "--port", str(port), "--docker-registry", registry_host]
        )
        resp = wait(
            lambda: requests.get(f"http://localhost:{port}/ping"),
            timeout_seconds=60,
            expected_exceptions=(requests.exceptions.ConnectionError,),
        )
        assert resp.text == "pong"

        yield CogServer(port=port, registry_host=registry_host)

    os.chdir(old_cwd)
    server_proc.kill()


@pytest.fixture
def project_dir(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "predict.py", "w") as f:
        f.write(
            """
import sys
import tempfile
from pathlib import Path
import cog

class Model(cog.Model):
    def setup(self):
        print("setting up model")
        self.foo = "foo"

    @cog.input("text", type=str)
    @cog.input("path", type=Path)
    @cog.input("output_file", type=bool, default=False)
    def predict(self, text, path, output_file):
        sys.stderr.write("processing " + text + "\\n")
        with open(path) as f:
            output = self.foo + text + f.read()
        if output_file:
            tmp = tempfile.NamedTemporaryFile(suffix=".txt")
            tmp.close()
            tmp_path = Path(tmp.name)
            with tmp_path.open("w") as f:
                f.write(output)
                print("successfully processed file " + text)
                return tmp_path
        print("successfully processed " + text)
        return output
        """
        )
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
name: andreas/hello-world
model: predict.py:Model
environment:
  architectures:
    - cpu
        """
        f.write(cog_yaml)

    with open(tmpdir / "myfile.txt", "w") as f:
        f.write("baz")

    return tmpdir


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
    subprocess.run(["docker", "rmi", image], check=False)
