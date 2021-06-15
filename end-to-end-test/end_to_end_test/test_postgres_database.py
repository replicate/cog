import json
from glob import glob
import os
import subprocess
import requests

from .util import (
    random_string,
    set_model_url,
    show_version,
    push_with_log,
    find_free_port,
    wait_for_http,
)


def test_postgres_end_to_end(
    registry_host, postgres_port, project_dir, subprocess_factory
):
    port = find_free_port()
    subprocess_factory.Popen(
        [
            "cog",
            "server",
            "--port",
            str(port),
            "--docker-registry",
            registry_host,
            "--database",
            "postgres",
            "--db-host",
            "localhost",
            "--db-port",
            str(postgres_port),
            "--db-user",
            "postgres",
            "--db-password",
            "postgres",
            "--db-name",
            "postgres",
        ]
    )
    resp = wait_for_http(f"http://localhost:{port}/ping")
    assert resp.text == "pong"

    user = random_string(10)
    model_name = random_string(10)
    model_url = f"http://localhost:{port}/{user}/{model_name}"
    set_model_url(model_url, project_dir)

    version_id = push_with_log(project_dir)
    out = show_version(model_url, version_id)

    assert out["id"] == version_id
    assert out["images"][0]["arch"] == "cpu"
    assert out["images"][0]["run_arguments"]["path"]["type"] == "Path"
    assert out["config"]["model"] == "predict.py:Model"
    assert out["config"]["examples"][2]["output"] == "@cog-example-output/output.02.txt"

    build_id = out["build_ids"]["cpu"]
    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "build", "log", build_id], stdout=subprocess.PIPE
    ).communicate()
    out = out.decode().strip()
    lines = out.splitlines()
    assert "Building image" in lines[0]
    assert "Successfully built image" in lines[-1]
    assert "Copying code" in out
    assert "Testing model" in out

    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "ls", "--quiet"], stdout=subprocess.PIPE
    ).communicate()
    version_ids = out.decode().strip().splitlines()
    assert version_ids == [version_id]

    with open(project_dir / "my_new_file.txt", "w") as f:
        f.write("i'm new")

    # test inserting a second version
    version_id2 = push_with_log(project_dir)
    out = show_version(model_url, version_id2)
    assert out["id"] == version_id2

    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "ls", "--quiet"], stdout=subprocess.PIPE
    ).communicate()
    version_ids = out.decode().strip().splitlines()
    assert version_ids == [version_id2, version_id]

    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "delete", version_id], stdout=subprocess.PIPE
    ).communicate()

    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "ls", "--quiet"], stdout=subprocess.PIPE
    ).communicate()
    version_ids = out.decode().strip().splitlines()
    assert version_ids == [version_id2]
