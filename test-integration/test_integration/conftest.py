import subprocess
import pytest

from .util import random_string, find_free_port, docker_run, get_local_ip, wait_for_port


@pytest.fixture
def project_dir(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "predict.py", "w") as f:
        f.write(
            """
import logging
import ctypes
import sys
import tempfile
from pathlib import Path
import cog
import time

libc = ctypes.CDLL(None)

# test that we can still capture type signature even if we write
# a bunch of stuff at import time.
libc.puts(b"writing some stuff from C at import time\\n")
sys.stdout.write("writing to stdout at import time\\n")
sys.stderr.write("writing to stderr at import time\\n")

class Predictor(cog.Predictor):
    def setup(self):
        print("setting up predictor")
        self.foo = "foo"

    @cog.input("text", type=str)
    @cog.input("path", type=Path)
    @cog.input("output_file", type=bool, default=False)
    def predict(self, text, path, output_file):
        logging.warn("writing log message")
        time.sleep(.1)
        libc.puts(b"writing from C")
        time.sleep(.1)
        sys.stderr.write("processing " + text + "\\n")
        time.sleep(.1)
        sys.stderr.flush()
        time.sleep(.1)
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
build:
predict: predict.py:Predictor
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
