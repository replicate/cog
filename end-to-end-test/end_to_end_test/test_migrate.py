import os
import subprocess

from .util import (
    random_string,
    set_model_url,
    show_version,
    push_with_log,
    find_free_port,
    wait_for_http,
)


def test_migrate(cog_server, postgres_port, project_dir, subprocess_factory):

    # start postgres-backed cog server
    port = find_free_port()
    subprocess_factory.Popen(
        [
            "cog",
            "server",
            "--port",
            str(port),
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

    # set up model
    user = random_string(10)
    model_name = random_string(10)
    model_url = f"http://localhost:{cog_server.port}/{user}/{model_name}"
    model_url_pg = f"http://localhost:{port}/{user}/{model_name}"

    # push model to local filesystem-backed cog server
    set_model_url(model_url, project_dir)
    version_id = push_with_log(project_dir)

    # run a dry migration to cog
    migrate_args = [
        "go",
        "run",
        "./tools/migrate_database/main.go",
        "--source-database",
        "filesystem",
        "--source-directory",
        os.path.join(cog_server.workdir, ".cog/database"),
        "--destination-database",
        "postgres",
        "--destination-host",
        "localhost",
        "--destination-port",
        str(postgres_port),
        "--destination-user",
        "postgres",
        "--destination-password",
        "postgres",
        "--destination-name",
        "postgres",
    ]
    subprocess.Popen(
        migrate_args + ["--dry"],
        cwd="..",
    ).communicate()

    # list models on fs-backed cog server
    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "ls", "--quiet"], stdout=subprocess.PIPE
    ).communicate()
    version_ids = out.decode().strip().splitlines()
    assert version_ids == [version_id]

    # list models on pg-backed cog server, should be none since migration was dry
    out, _ = subprocess.Popen(
        ["cog", "--model", model_url_pg, "ls", "--quiet"], stdout=subprocess.PIPE
    ).communicate()
    version_ids_pg = out.decode().strip().splitlines()
    assert version_ids_pg == []

    # do a real migration
    subprocess.Popen(
        migrate_args,
        cwd="..",
    ).communicate()

    # list models on pg-backed cog server, should now be same as on fs-backed cog server
    out, _ = subprocess.Popen(
        ["cog", "--model", model_url_pg, "ls", "--quiet"], stdout=subprocess.PIPE
    ).communicate()
    version_ids_pg = out.decode().strip().splitlines()

    assert version_ids_pg == version_ids

    # check that all the version and image data were migrated
    version = show_version(model_url, version_id)
    version_pg = show_version(model_url_pg, version_id)
    assert version == version_pg

    # check that build logs were migrated
    logs = subprocess.Popen(
        ["cog", "--model", model_url, "build", "log", version["build_ids"]["cpu"]],
        stdout=subprocess.PIPE,
    ).communicate()[0].decode()
    logs_pg = subprocess.Popen(
        ["cog", "--model", model_url_pg, "build", "log", version["build_ids"]["cpu"]],
        stdout=subprocess.PIPE,
    ).communicate()[0].decode()

    assert "Building image" in logs
    assert logs == logs_pg

    # do another migration to test idempotence
    subprocess.Popen(
        migrate_args,
        cwd="..",
    ).communicate()
