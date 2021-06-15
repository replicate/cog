from typing import Generator
from dataclasses import dataclass
import os
import subprocess
import psycopg2
import pytest

from .util import (
    random_string,
    find_free_port,
    docker_run,
    wait_for_port,
    wait_for_http,
    retry,
)


@dataclass
class CogServer:
    port: int
    registry_host: str
    workdir: str


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
def subprocess_factory():
    factory = SubprocessFactory()
    yield factory
    factory.kill()


@pytest.fixture
def cog_server(
    registry_host, subprocess_factory, tmpdir_factory
) -> Generator[CogServer, None, None]:
    port = find_free_port()
    tmpdir = tmpdir_factory.mktemp("tempwd")
    subprocess_factory.Popen(
        ["cog", "server", "--port", str(port), "--docker-registry", registry_host], cwd=tmpdir
    )
    resp = wait_for_http(f"http://localhost:{port}/ping")
    assert resp.text == "pong"

    yield CogServer(port=port, registry_host=registry_host, workdir=tmpdir)


@pytest.fixture
def project_dir(tmpdir_factory):
    tmpdir = tmpdir_factory.mktemp("project")
    with open(tmpdir / "predict.py", "w") as f:
        f.write(
            """
import tempfile
from pathlib import Path
import cog

class Model(cog.Model):
    def setup(self):
        self.foo = "foo"

    @cog.input("text", type=str)
    @cog.input("path", type=Path)
    @cog.input("output_file", type=bool, default=False)
    def predict(self, text, path, output_file):
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
model: predict.py:Model
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


@pytest.fixture
def postgres_port():
    container_name = "cog-test-postgres-" + random_string(10)
    port = find_free_port()
    with docker_run(
        "postgres",
        name=container_name,
        publish=[{"host": port, "container": 5432}],
        detach=True,
        env={
            "POSTGRES_PASSWORD": "postgres",
        },
    ):
        wait_for_port("localhost", port)
        connect = lambda: psycopg2.connect(
            f"dbname=postgres user=postgres host=localhost port={port} password=postgres",
        )
        conn = retry(connect, retries=10, sleep=1)
        cur = conn.cursor()
        cur.execute(
            """
CREATE TABLE versions (
    id          TEXT            NOT NULL,
    username    TEXT            NOT NULL,
    model_name  TEXT            NOT NULL,
    data        JSON            NOT NULL,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, username, model_name)
);
"""
        )
        cur.execute(
            """
CREATE TABLE images (
    version_id  TEXT            NOT NULL,
    username    TEXT            NOT NULL,
    model_name  TEXT            NOT NULL,
    arch        TEXT            NOT NULL,
    data        JSON            NOT NULL,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (version_id, username, model_name, arch)
);
"""
        )
        cur.execute(
            """
CREATE TABLE build_log_lines (
    id          SERIAL          NOT NULL PRIMARY KEY,
    username    TEXT            NOT NULL,
    model_name  TEXT            NOT NULL,
    build_id    TEXT            NOT NULL,
    level       INT             NOT NULL DEFAULT 0,
    line        TEXT            NOT NULL DEFAULT '',
    done        BOOL            NOT NULL DEFAULT FALSE,
    timestamp_nano BIGINT       NOT NULL
);
"""
        )
        conn.commit()
        yield port


class SubprocessFactory:
    procs = []

    def Popen(self, *args, **kwargs):
        self.procs.append(subprocess.Popen(*args, **kwargs))

    def kill(self):
        for proc in self.procs:
            proc.kill()
