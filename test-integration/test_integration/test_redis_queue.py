import json
from pathlib import Path
import subprocess

from .util import docker_run, random_string


def test_queue_worker_files(docker_image, docker_network, redis_client, upload_server):
    project_dir = Path(__file__).parent / "fixtures/file-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    with open(upload_server / "input.txt", "w") as f:
        f.write("test")

    with docker_run(
        image=docker_image,
        interactive=True,
        network=docker_network,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            "redis",
            "6379",
            "predict-queue",
            "http://upload-server:5000/upload",
            "test-worker",
            "model_id",
            "logs",
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "text": {"value": "baz"},
                            "path": {
                                "file": {
                                    "name": "input.txt",
                                    "url": "http://upload-server:5000/download/input.txt",
                                }
                            },
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )
        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "value": "http://upload-server:5000/download/output.txt",
            "status": "success",
        }

        with open(upload_server / "output.txt") as f:
            assert f.read() == "foobaztest"


def test_queue_worker_yielding(docker_network, docker_image, redis_client):
    project_dir = Path(__file__).parent / "fixtures/yielding-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    with docker_run(
        image=docker_image,
        interactive=True,
        network=docker_network,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            "redis",
            "6379",
            "predict-queue",
            "",
            "test-worker",
            "model_id",
            "logs",
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "text": {"value": "bar"},
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"value": "foo", "status": "processing"}

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"value": "bar", "status": "processing"}

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"value": "baz", "status": "success"}

        response = redis_client.rpop("response-queue")
        assert response == None


def test_queue_worker_error(docker_network, docker_image, redis_client):
    project_dir = Path(__file__).parent / "fixtures/failing-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    with docker_run(
        image=docker_image,
        interactive=True,
        network=docker_network,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            "redis",
            "6379",
            "predict-queue",
            "",
            "test-worker",
            "model_id",
            "logs",
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "text": {"value": "bar"},
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "failed", "error": "over budget"}

        response = redis_client.rpop("response-queue")
        assert response == None


def test_queue_worker_logging(docker_network, docker_image, redis_client):
    project_dir = Path(__file__).parent / "fixtures/logging-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    with docker_run(
        image=docker_image,
        interactive=True,
        network=docker_network,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            "redis",
            "6379",
            "predict-queue",
            "",
            "test-worker",
            "model_id",
            "logs",
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {},
                        "response_queue": "response-queue",
                    }
                ),
            },
        )
        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "success", "value": "output"}

        setup_log_lines = []
        run_log_lines = []
        while True:
            raw_entry = redis_client.lpop("logs")
            if not raw_entry:
                break
            entry = json.loads(raw_entry)
            stage = entry["stage"]
            line = entry["line"]
            if stage == "setup":
                setup_log_lines.append(line)
            else:
                run_log_lines.append(line)

        assert setup_log_lines == ["setting up predictor"]
        assert run_log_lines == [
            "WARNING:root:writing log message",
            "writing from C",
            "writing to stderr",
            "writing with print",
        ]


def test_queue_worker_timeout(docker_network, docker_image, redis_client):
    project_dir = Path(__file__).parent / "fixtures/timeout-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    with docker_run(
        image=docker_image,
        interactive=True,
        network=docker_network,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            "redis",
            "6379",
            "predict-queue",
            "",
            "test-worker",
            "model_id",
            "logs",
            "1",  # timeout
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 0.5},
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "success", "value": "it worked!"}

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 5.0},
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "failed", "error": "Prediction timed out"}


def test_queue_worker_yielding_timeout(docker_image, docker_network, redis_client):
    project_dir = Path(__file__).parent / "fixtures/yielding-timeout-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    with docker_run(
        image=docker_image,
        interactive=True,
        network=docker_network,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            "redis",
            "6379",
            "predict-queue",
            "",
            "test-worker",
            "model_id",
            "logs",
            "1",  # timeout
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 0.5},
                            "n_iterations": {"value": 1},
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "success", "value": "yield 0"}

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 0.7},
                            "n_iterations": {"value": 10},
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        # TODO(andreas): revisit this test design if it starts being flakey
        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"value": "yield 0", "status": "processing"}

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "failed", "error": "Prediction timed out"}
