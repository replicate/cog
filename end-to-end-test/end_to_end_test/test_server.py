from glob import glob
import os
from os.path import basename
import tempfile
import socket
from contextlib import closing
import subprocess

import pytest
import requests
from waiting import wait


@pytest.fixture
def cog_server_port_dir():
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

        out, err = subprocess.Popen(
            ["cog", "remote"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        ).communicate()
        old_remote = out.decode().strip()

        out, err = subprocess.Popen(
            ["cog", "remote", "set", "http://localhost:" + port],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        ).communicate()
        assert out.decode() == f"Updated remote: http://localhost:{port}\n"
        assert err == b""

        yield port, cog_dir

    os.chdir(old_cwd)
    server_proc.kill()

    subprocess.Popen(["cog", "remote", "set", old_remote]).communicate()


def test_build_show_list_download_infer(cog_server_port_dir, tmpdir_factory):
    cog_port, cog_dir = cog_server_port_dir
    project_dir = tmpdir_factory.mktemp("project")
    with open(project_dir / "infer.py", "w") as f:
        f.write(
            """
import cog

class Model(cog.Model):
    def setup(self):
        self.foo = "foo"

    @cog.input("text", type=str)
    def run(self, text):
        return self.foo + text
        """
        )
    with open(project_dir / "cog.yaml", "w") as f:
        f.write(
            """
name: andreas/hello-world
model: infer.py:Model
examples:
  - input:
      text: "foo"
    output: "foofoo"
  - input:
      text: "bar"
    output: "foobar"
environment:
  architectures:
    - cpu
        """
        )

    out, _ = subprocess.Popen(
        ["cog", "build"],
        cwd=project_dir,
        stdout=subprocess.PIPE,
    ).communicate()

    assert out.decode().startswith("Successfully built "), (
        out.decode() + " doesn't start with 'Successfully built'"
    )
    package_id = out.decode().strip().split("Successfully built ")[1]

    out, _ = subprocess.Popen(
        ["cog", "show", package_id], stdout=subprocess.PIPE
    ).communicate()
    lines = out.decode().splitlines()
    assert lines[0] == f"ID:       {package_id}"
    assert lines[1] == "Name:     andreas/hello-world"

    out, _ = subprocess.Popen(["cog", "ls"], stdout=subprocess.PIPE).communicate()
    lines = out.decode().splitlines()
    assert lines[1].startswith(f"{package_id}  andreas/hello-world")

    download_dir = tmpdir_factory.mktemp("download") / "my-dir"
    subprocess.Popen(
        ["cog", "download", "--output-dir", download_dir, package_id],
        stdout=subprocess.PIPE,
    ).communicate()
    paths = sorted(glob(str(download_dir / "*.*")))
    filenames = [os.path.basename(f) for f in paths]
    assert filenames == ["cog.yaml", "infer.py"]

    output_dir = tmpdir_factory.mktemp("output")
    out_path = output_dir / "out.txt"
    subprocess.Popen(
        ["cog", "infer", "-o", out_path, "-i", "text=baz", package_id],
        stdout=subprocess.PIPE,
    ).communicate()
    with out_path.open() as f:
        assert f.read() == "foobaz"


def find_free_port():
    with closing(socket.socket(socket.AF_INET, socket.SOCK_STREAM)) as s:
        s.bind(("", 0))
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        return s.getsockname()[1]
