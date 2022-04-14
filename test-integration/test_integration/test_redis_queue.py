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
            "status": "succeeded",
            "output": "http://upload-server:5000/download/output.txt",
            "logs": [],
        }

        with open(upload_server / "output.txt") as f:
            assert f.read() == "foobaztest"


def test_queue_worker_yielding_file(
    docker_network, docker_image, redis_client, upload_server
):
    project_dir = Path(__file__).parent / "fixtures/yielding-file-project"
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
            "status": "processing",
            "output": ["http://upload-server:5000/download/out-0.txt"],
            "logs": [],
        }

        with open(upload_server / "out-0.txt") as f:
            assert f.read() == "test foo"

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "processing",
            "output": [
                "http://upload-server:5000/download/out-0.txt",
                "http://upload-server:5000/download/out-1.txt",
            ],
            "logs": [],
        }

        with open(upload_server / "out-1.txt") as f:
            assert f.read() == "test bar"

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "processing",
            "output": [
                "http://upload-server:5000/download/out-0.txt",
                "http://upload-server:5000/download/out-1.txt",
                "http://upload-server:5000/download/out-2.txt",
            ],
            "logs": [],
        }

        with open(upload_server / "out-2.txt") as f:
            assert f.read() == "test baz"

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "succeeded",
            "output": [
                "http://upload-server:5000/download/out-0.txt",
                "http://upload-server:5000/download/out-1.txt",
                "http://upload-server:5000/download/out-2.txt",
            ],
            "logs": [],
        }

        response = redis_client.rpop("response-queue")
        assert response == None


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
        assert response == {
            "status": "processing",
            "output": ["foo", "bar", "baz"],
            "logs": [],
        }

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "succeeded",
            "output": ["foo", "bar", "baz"],
            "logs": [],
        }

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
        assert response == {
            "status": "failed",
            "output": None,
            "logs": [],
            "error": "over budget",
        }

        response = redis_client.rpop("response-queue")
        assert response == None


def test_queue_worker_error_after_output(docker_network, docker_image, redis_client):
    project_dir = Path(__file__).parent / "fixtures/failing-after-output-project"
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
        assert response == {
            "status": "processing",
            "output": ["hello bar"],
            "logs": [],
        }

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "processing",
            "output": ["hello bar"],
            "logs": ["a printed log message"],
        }

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "failed",
            "output": ["hello bar"],
            "logs": ["a printed log message"],
            "error": "mid run error",
        }

        response = redis_client.rpop("response-queue")
        assert response == None


def test_queue_worker_invalid_input(docker_network, docker_image, redis_client):
    project_dir = Path(__file__).parent / "fixtures/int-project"
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
                            "input": {"value": "not a number"},
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert "status" in response
        assert response["status"] == "failed"

        assert "error" in response
        assert "value is not a valid integer" in response["error"]


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
        assert response == {
            "status": "processing",
            "output": None,
            "logs": [
                "WARNING:root:writing log message",
            ],
        }

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "processing",
            "output": None,
            "logs": [
                "WARNING:root:writing log message",
                "writing from C",
            ],
        }

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "processing",
            "output": None,
            "logs": [
                "WARNING:root:writing log message",
                "writing from C",
                "writing to stderr",
            ],
        }

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "processing",
            "output": None,
            "logs": [
                "WARNING:root:writing log message",
                "writing from C",
                "writing to stderr",
                "writing with print",
            ],
        }

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "succeeded",
            "output": "output",
            "logs": [
                "WARNING:root:writing log message",
                "writing from C",
                "writing to stderr",
                "writing with print",
            ],
        }

        response = redis_client.rpop("response-queue")
        assert response == None


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
        assert response == {"status": "succeeded", "output": "it worked!", "logs": []}

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
        assert response == {"status": "processing", "output": ["yield 0"], "logs": []}

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "succeeded", "output": ["yield 0"], "logs": []}

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 0.4},
                            "n_iterations": {"value": 10},
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        # TODO(andreas): revisit this test design if it starts being flakey
        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "processing", "output": ["yield 0"], "logs": []}

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {
            "status": "processing",
            "output": ["yield 0", "yield 1"],
            "logs": [],
        }

        response = json.loads(redis_client.brpop("response-queue", timeout=10)[1])
        assert response == {"status": "failed", "error": "Prediction timed out"}
