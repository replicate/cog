import json
from pathlib import Path
import re
import subprocess
import time
import unittest.mock as mock

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
                        "input": {
                            "text": "baz",
                            "path": "http://upload-server:5000/download/input.txt",
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": "http://upload-server:5000/download/output.txt",
            "logs": [],
        }

        assert re.match(
            r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{6}",
            response["x-experimental-timestamps"]["started_at"],
        )
        assert re.match(
            r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{6}",
            response["x-experimental-timestamps"]["completed_at"],
        )

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
                        "input": {
                            "path": "http://upload-server:5000/download/input.txt",
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": ["http://upload-server:5000/download/out-0.txt"],
            "logs": [],
        }

        with open(upload_server / "out-0.txt") as f:
            assert f.read() == "test foo"

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": [
                "http://upload-server:5000/download/out-0.txt",
                "http://upload-server:5000/download/out-1.txt",
            ],
            "logs": [],
        }

        with open(upload_server / "out-1.txt") as f:
            assert f.read() == "test bar"

        next(responses)  # discard the intermediate response
        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": [
                "http://upload-server:5000/download/out-0.txt",
                "http://upload-server:5000/download/out-1.txt",
                "http://upload-server:5000/download/out-2.txt",
            ],
            "logs": [],
        }

        with open(upload_server / "out-2.txt") as f:
            assert f.read() == "test baz"


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
                        "input": {
                            "text": "bar",
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": ["foo", "bar", "baz"],
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": ["foo", "bar", "baz"],
            "logs": [],
        }


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
                        "input": {
                            "text": "bar",
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        next(responses)  # discard the intermediate response
        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "failed",
            "output": None,
            "logs": mock.ANY,  # includes a stack trace
            "error": "over budget",
        }
        assert "Traceback (most recent call last):" in response["logs"]


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
                        "input": {
                            "text": "bar",
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": ["hello bar"],
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": ["hello bar"],
            "logs": ["a printed log message"],
        }

        next(responses)  # discard the intermediate response
        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "failed",
            "output": ["hello bar"],
            "logs": mock.ANY,  # includes a stack trace
            "error": "mid run error",
        }
        assert "Traceback (most recent call last):" in response["logs"]


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
                        "input": {
                            "num": "not a number",
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "status": "failed",
            "output": None,
            "logs": [],
            "error": mock.ANY,
        }
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
                        "input": {},
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [
                "WARNING:root:writing log message",
            ],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [
                "WARNING:root:writing log message",
                "writing from C",
            ],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [
                "WARNING:root:writing log message",
                "writing from C",
                "writing to stderr",
            ],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [
                "WARNING:root:writing log message",
                "writing from C",
                "writing to stderr",
                "writing with print",
            ],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": "output",
            "logs": [
                "WARNING:root:writing log message",
                "writing from C",
                "writing to stderr",
                "writing with print",
            ],
        }


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
            "2",  # timeout
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
                        "input": {
                            "sleep_time": 0.1,
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": "it worked after 0.1 seconds!",
            "logs": [],
        }

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "input": {
                            "sleep_time": 3.0,
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "failed",
            "output": None,
            "logs": [],
            "error": "Prediction timed out",
        }

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "input": {
                            "sleep_time": 0.2,
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": "it worked after 0.2 seconds!",
            "logs": [],
        }


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
            "2",  # timeout
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
                        "input": {
                            "sleep_time": 0.1,
                            "n_iterations": 1,
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        next(responses)  # discard the intermediate response
        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": ["yield 0"],
            "logs": [],
        }

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "input": {
                            "sleep_time": 0.8,
                            "n_iterations": 10,
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        # TODO(andreas): revisit this test design if it starts being flakey
        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": ["yield 0"],
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": ["yield 0", "yield 1"],
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "failed",
            "output": ["yield 0", "yield 1"],
            "logs": [],
            "error": "Prediction timed out",
        }


def test_queue_worker_complex_output(docker_network, docker_image, redis_client):
    project_dir = Path(__file__).parent / "fixtures/complex-output-project"
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
                        "input": {
                            "name": "world",
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": {
                "hello": "hello world",
                "goodbye": "goodbye world",
            },
            "logs": [],
        }


# Testing make_pickable works with sufficiently complex things.
# We're also testing uploading files because that is a separate code path in the make redis worker.
# Shame this is an integration test but want to make sure this works for erlich without loads of manual testing.
# Maybe this can be removed when we have better unit test coverage for redis things.
def test_queue_worker_yielding_list_of_complex_output(
    docker_network, docker_image, redis_client, upload_server
):
    project_dir = (
        Path(__file__).parent / "fixtures/yielding-list-of-complex-output-project"
    )
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
                        "input": {},
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": None,
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
            },
            "status": "processing",
            "output": [
                [{"file": "http://upload-server:5000/download/file", "text": "hello"}]
            ],
            "logs": [],
        }

        response = next(responses)
        assert response == {
            "x-experimental-timestamps": {
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            "status": "succeeded",
            "output": [
                [{"file": "http://upload-server:5000/download/file", "text": "hello"}]
            ],
            "logs": [],
        }

        with open(upload_server / "file") as f:
            assert f.read() == "hello"


# the worker shouldn't start taking jobs until the runner has finished setup
def test_queue_worker_setup(docker_network, docker_image, redis_client):
    project_dir = Path(__file__).parent / "fixtures/long-setup-project"
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
                        "input": {},
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "input": {},
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "input": {},
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        # give it about five seconds to get properly into setup
        time.sleep(5)
        predictions_in_progress = redis_client.xpending(
            name="predict-queue", groupname="predict-queue"
        )["pending"]
        assert predictions_in_progress == 0

        # give it another 10s to finish setup
        time.sleep(10)
        predictions_in_progress = redis_client.xpending(
            name="predict-queue", groupname="predict-queue"
        )["pending"]
        assert predictions_in_progress == 1


def response_iterator(redis_client, response_queue, timeout=10):
    redis_client.config_set("notify-keyspace-events", "KEA")
    channel = redis_client.pubsub()
    channel.subscribe(f"__keyspace@0__:{response_queue}")

    while True:
        start = time.time()

        while time.time() - start < timeout:
            message = channel.get_message()
            if message and message["data"] == b"set":
                yield json.loads(redis_client.get(response_queue))
            time.sleep(0.01)

        raise TimeoutError("Timed out waiting for Redis message")
