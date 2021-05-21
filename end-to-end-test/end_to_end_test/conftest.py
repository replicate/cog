import os
import subprocess
import tempfile
from waiting import wait
import requests
import pytest

from .util import random_string, find_free_port, docker_run


@pytest.fixture
def cog_server_port():
    old_cwd = os.getcwd()
    with tempfile.TemporaryDirectory() as cog_dir:
        os.chdir(cog_dir)
        port = str(find_free_port())
        server_proc = subprocess.Popen(["cog", "server", "--port", port])
        resp = wait(
            lambda: requests.get("http://localhost:" + port + "/ping"),
            timeout_seconds=60,
            expected_exceptions=(requests.exceptions.ConnectionError,),
        )
        assert resp.text == "pong"

        yield port

    os.chdir(old_cwd)
    server_proc.kill()


@pytest.fixture
def project_dir(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "infer.py", "w") as f:
        f.write(
            """
import time
import tempfile
from pathlib import Path
import cog

class Model(cog.Model):
    def setup(self):
        self.foo = "foo"

    @cog.input("text", type=str)
    @cog.input("path", type=Path)
    @cog.input("output_file", type=bool, default=False)
    def run(self, text, path, output_file):
        time.sleep(1)
        with open(path) as f:
            output = self.foo + text + f.read()
        if output_file:
            tmp = tempfile.NamedTemporaryFile(suffix=".txt")
            tmp.close()
            tmp_path = Path(tmp.name)
            with tmp_path.open("w") as f:
                f.write(output)
                return tmp_path
        return output
        """
        )
    with open(tmpdir / "cog.yaml", "w") as f:
        cog_yaml = """
name: andreas/hello-world
model: infer.py:Model
examples:
  - input:
      text: "foo"
      path: "@myfile.txt"
    output: "foofoobaz"
  - input:
      text: "bar"
      path: "@myfile.txt"
    output: "foobarbaz"
  - input:
      text: "qux"
      path: "@myfile.txt"
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
        yield port
