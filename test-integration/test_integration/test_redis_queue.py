import json
import os
from pathlib import Path
import pytest
import re
import socket
import subprocess
import time
import unittest.mock as mock

from .util import docker_run, random_string

DEFAULT_ENV = {
    "COG_THROTTLE_RESPONSE_INTERVAL": "0",
}


@pytest.fixture(scope="session")
def httpserver_listen_address():
    if os.getenv("GITHUB_ACTIONS") == "true":
        # we can't use host.docker.internal, because it doesn't work on GitHub actions
        return (LOCAL_IP_ADDRESS, None)
    else:
        # but, using the host's local IP doesn't work locally, so use defaults there
        return (None, None)


def test_queue_worker_files(
    docker_image, docker_network, redis_client, upload_server, httpserver
):
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
            "--redis-host=redis",
            "--redis-port=6379",
            "--input-queue=predict-queue",
            "--upload-url=http://upload-server:5000/upload",
            "--consumer-id=test-worker",
            "--model-id=model_id",
        ],
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        # we expect a webhook on starting
        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "baz",
                    "path": "http://upload-server:5000/files/input.txt",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        final_response = None

        def capture_final_response(request):
            nonlocal final_response
            final_response = request.get_json()

        # and another on finishing
        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "baz",
                    "path": "http://upload-server:5000/files/input.txt",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": "http://upload-server:5000/files/output.txt",
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        ).respond_with_handler(capture_final_response)

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "text": "baz",
                                "path": "http://upload-server:5000/files/input.txt",
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result

        assert re.match(
            r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{6}Z",
            final_response["started_at"],
        )
        assert re.match(
            r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{6}Z",
            final_response["completed_at"],
        )
        assert type(final_response["metrics"]["predict_time"]) == float

        with open(upload_server / "output.txt") as f:
            assert f.read() == "foobaztest"


def test_queue_worker_yielding_file(
    docker_network, docker_image, redis_client, upload_server, httpserver
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "path": "http://upload-server:5000/files/input.txt",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "path": "http://upload-server:5000/files/input.txt",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": ["http://upload-server:5000/files/out-0.txt"],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "path": "http://upload-server:5000/files/input.txt",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": [
                    "http://upload-server:5000/files/out-0.txt",
                    "http://upload-server:5000/files/out-1.txt",
                ],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "path": "http://upload-server:5000/files/input.txt",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": [
                    "http://upload-server:5000/files/out-0.txt",
                    "http://upload-server:5000/files/out-1.txt",
                    "http://upload-server:5000/files/out-2.txt",
                ],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "path": "http://upload-server:5000/files/input.txt",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": [
                    "http://upload-server:5000/files/out-0.txt",
                    "http://upload-server:5000/files/out-1.txt",
                    "http://upload-server:5000/files/out-2.txt",
                ],
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        )

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "path": "http://upload-server:5000/files/input.txt",
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result

        with open(upload_server / "out-0.txt") as f:
            assert f.read() == "test foo"
        with open(upload_server / "out-1.txt") as f:
            assert f.read() == "test bar"
        with open(upload_server / "out-2.txt") as f:
            assert f.read() == "test baz"


def test_queue_worker_yielding(docker_network, docker_image, redis_client, httpserver):
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        for output in [["foo"], ["foo", "bar"], ["foo", "bar", "baz"]]:
            httpserver.expect_oneshot_request(
                "/webhook",
                json={
                    "id": predict_id,
                    "input": {
                        "text": "bar",
                    },
                    "webhook": webhook_url,
                    "logs": "",
                    "output": output,
                    "status": "processing",
                    "started_at": mock.ANY,
                },
                method="POST",
            )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": ["foo", "bar", "baz"],
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        )

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "text": "bar",
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result


def test_queue_worker_error(docker_network, docker_image, redis_client, httpserver):
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        # There's a timing issue with this test. Locally, this request doesn't
        # make it, because the stack trace logs never come through. On GitHub
        # actions, the stack trace logs *do* come through. Set up a request
        # handler which can be, but does not have to be, called.
        httpserver.expect_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": mock.ANY,  # includes a stack trace
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        final_response = None

        def capture_final_response(request):
            nonlocal final_response
            final_response = request.get_json()

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "error": "over budget",
                "logs": mock.ANY,  # might include a stack trace (see above)
                "output": None,
                "status": "failed",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            method="POST",
        ).respond_with_handler(capture_final_response)

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "text": "bar",
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result


def test_queue_worker_error_after_output(
    docker_network, docker_image, redis_client, httpserver
):
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": ["hello bar"],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": "a printed log message\n",
                "output": ["hello bar"],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        # There's a timing issue with this test. Sometimes (rarely?) on GitHub
        # actions, the stack trace logs don't make it. Set up a request handler
        # which can be, but does not have to be, called.
        httpserver.expect_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": mock.ANY,  # includes a stack trace
                "output": ["hello bar"],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        final_response = None

        def capture_final_response(request):
            nonlocal final_response
            final_response = request.get_json()

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "error": "mid run error",
                "logs": mock.ANY,  # might include a stack trace
                "output": ["hello bar"],
                "status": "failed",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            method="POST",
        ).respond_with_handler(capture_final_response)

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "text": "bar",
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result

        # TODO Debug timing issue so we can reliably assert that tracebacks get logged
        # assert "Traceback (most recent call last):" in final_response["logs"]


def test_queue_worker_unhandled_error(
    docker_network, docker_image, redis_client, httpserver
):
    project_dir = Path(__file__).parent / "fixtures/unhandled-error-project"
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        # There's a timing issue with this test. Locally, this request doesn't
        # make it, because the stack trace logs never come through. On GitHub
        # actions, the stack trace logs *do* come through. Set up a request
        # handler which can be, but does not have to be, called.
        httpserver.expect_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "logs": mock.ANY,  # includes a stack trace
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        final_response = None

        def capture_final_response(request):
            nonlocal final_response
            final_response = request.get_json()

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "text": "bar",
                },
                "webhook": webhook_url,
                "error": "Prediction failed for an unknown reason. It might have run out of memory? (exitcode 1)",
                "logs": mock.ANY,  # might include a stack trace (see above)
                "output": None,
                "status": "failed",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            method="POST",
        ).respond_with_handler(capture_final_response)

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "text": "bar",
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result


def test_queue_worker_invalid_input(
    docker_network, docker_image, redis_client, httpserver
):
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        final_response = None

        def capture_final_response(request):
            nonlocal final_response
            final_response = request.get_json()

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "num": "not a number",
                },
                "webhook": webhook_url,
                "error": mock.ANY,
                "logs": "",
                "output": None,
                "status": "failed",
            },
            method="POST",
        ).respond_with_handler(capture_final_response)

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "num": "not a number",
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result

        assert "value is not a valid integer" in final_response["error"]


def test_queue_worker_logging(docker_network, docker_image, redis_client, httpserver):
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": "WARNING:root:writing log message\n",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": "WARNING:root:writing log message\nwriting from C\n",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": (
                    "WARNING:root:writing log message\n"
                    + "writing from C\n"
                    + "writing to stderr\n"
                ),
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": (
                    "WARNING:root:writing log message\n"
                    + "writing from C\n"
                    + "writing to stderr\n"
                    + "writing with print\n"
                ),
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": (
                    "WARNING:root:writing log message\n"
                    + "writing from C\n"
                    + "writing to stderr\n"
                    + "writing with print\n"
                ),
                "output": "output",
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        )

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {},
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result


def test_queue_worker_timeout(docker_network, docker_image, redis_client, httpserver):
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
            "--redis-host=redis",
            "--redis-port=6379",
            "--input-queue=predict-queue",
            "--upload-url=",
            "--consumer-id=test-worker",
            "--model-id=model_id",
            "--predict-timeout=2",
        ],
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.1,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.1,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": "it worked after 0.1 seconds!",
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        ).respond_with_data("OK")

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "sleep_time": 0.1,
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result

        predict_id = random_string(10)

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 3.0,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 3.0,
                },
                "webhook": webhook_url,
                "error": "Prediction timed out",
                "logs": "",
                "output": None,
                "status": "failed",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "sleep_time": 3.0,
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result

        predict_id = random_string(10)

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.2,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.2,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": "it worked after 0.2 seconds!",
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        ).respond_with_data("OK")

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "sleep_time": 0.2,
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result


def test_queue_worker_yielding_timeout(
    docker_image, docker_network, redis_client, httpserver
):
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.1,
                    "n_iterations": 1,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.1,
                    "n_iterations": 1,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": ["yield 0"],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.1,
                    "n_iterations": 1,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": ["yield 0"],
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        ).respond_with_data("OK")

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
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
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result

        predict_id = random_string(10)

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.8,
                    "n_iterations": 10,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.8,
                    "n_iterations": 10,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": ["yield 0"],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.8,
                    "n_iterations": 10,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": ["yield 0", "yield 1"],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 0.8,
                    "n_iterations": 10,
                },
                "webhook": webhook_url,
                "error": "Prediction timed out",
                "logs": "",
                "output": ["yield 0", "yield 1"],
                "status": "failed",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("OK")

        with httpserver.wait(timeout=15) as waiting:
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
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result


def test_queue_worker_complex_output(
    docker_network, docker_image, redis_client, httpserver
):
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "name": "world",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "name": "world",
                },
                "webhook": webhook_url,
                "logs": "",
                "output": {
                    "hello": "hello world",
                    "goodbye": "goodbye world",
                },
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        )

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "name": "world",
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result


# Testing make_pickable works with sufficiently complex things.
# We're also testing uploading files because that is a separate code path in the make redis worker.
# Shame this is an integration test but want to make sure this works for erlich without loads of manual testing.
# Maybe this can be removed when we have better unit test coverage for redis things.
def test_queue_worker_yielding_list_of_complex_output(
    docker_network, docker_image, redis_client, upload_server, httpserver
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": "",
                "output": [
                    [
                        {
                            "file": "http://upload-server:5000/files/file",
                            "text": "hello",
                        }
                    ]
                ],
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {},
                "webhook": webhook_url,
                "logs": "",
                "output": [
                    [
                        {
                            "file": "http://upload-server:5000/files/file",
                            "text": "hello",
                        }
                    ]
                ],
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        )

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {},
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result

        with open(upload_server / "file") as f:
            assert f.read() == "hello"


# the worker shouldn't start taking jobs until the runner has finished setup
def test_queue_worker_setup(docker_network, docker_image, redis_client, httpserver):
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
        env=DEFAULT_ENV,
    ):
        httpserver.expect_request("/webhook", method="POST")
        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )
        redis_client.xadd(
            name="predict-queue",
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "input": {},
                        "webhook": webhook_url,
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
                        "webhook": webhook_url,
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
                        "webhook": webhook_url,
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


def test_queue_worker_webhook_retries(
    docker_network, docker_image, redis_client, httpserver
):
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
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        # respond with an error to the initial response -- it shouldn't be retried
        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "num": 8,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
            },
            method="POST",
        ).respond_with_data("error", status=500)

        # respond with an error to the terminal response ...
        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "num": 8,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": 16,
                "status": "succeeded",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "metrics": {
                    "predict_time": mock.ANY,
                },
            },
            method="POST",
        ).respond_with_data("error", status=500)

        # ... it should be retried several times
        for x in range(3):
            httpserver.expect_oneshot_request(
                "/webhook",
                json={
                    "id": predict_id,
                    "input": {
                        "num": 8,
                    },
                    "webhook": webhook_url,
                    "logs": "",
                    "output": 16,
                    "status": "succeeded",
                    "started_at": mock.ANY,
                    "completed_at": mock.ANY,
                    "metrics": {
                        "predict_time": mock.ANY,
                    },
                },
                method="POST",
            )

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=30) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "num": 8,
                            },
                            "webhook": webhook_url,
                        }
                    ),
                },
            )

        # check we received all the webhooks
        assert waiting.result


def test_queue_worker_redis_responses(docker_network, docker_image, redis_client):
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
        env=DEFAULT_ENV,
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
                            "num": 42,
                        },
                        "response_queue": "response-queue",
                    }
                ),
            },
        )

        responses = response_iterator(redis_client, "response-queue")

        # Discard the initial response -- depending on the speed of the test
        # runner, the second response can come before we've had a chance to
        # read it. This asserts a response happened, but not what it contains.
        response = next(responses)

        response = next(responses)
        assert response == {
            "id": predict_id,
            "input": {
                "num": 42,
            },
            "response_queue": "response-queue",
            "logs": "",
            "output": 84,
            "status": "succeeded",
            "started_at": mock.ANY,
            "completed_at": mock.ANY,
            "metrics": {
                "predict_time": mock.ANY,
            },
        }


def test_queue_worker_cancel(docker_network, docker_image, redis_client, httpserver):
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
        ],
        env=DEFAULT_ENV,
    ):
        predict_id = random_string(10)
        webhook_url = httpserver.url_for("/webhook").replace(
            "localhost", "host.docker.internal"
        )

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 30,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "processing",
                "started_at": mock.ANY,
                "cancel_key": "cancel-key",
            },
            method="POST",
        )

        redis_client.xgroup_create(
            mkstream=True, groupname="predict-queue", name="predict-queue", id="$"
        )

        with httpserver.wait(timeout=15) as waiting:
            redis_client.xadd(
                name="predict-queue",
                fields={
                    "value": json.dumps(
                        {
                            "id": predict_id,
                            "input": {
                                "sleep_time": 30,
                            },
                            "webhook": webhook_url,
                            "cancel_key": "cancel-key",
                        }
                    ),
                },
            )

        # check we receive the initial webhook
        assert waiting.result

        httpserver.expect_oneshot_request(
            "/webhook",
            json={
                "id": predict_id,
                "input": {
                    "sleep_time": 30,
                },
                "webhook": webhook_url,
                "logs": "",
                "output": None,
                "status": "canceled",
                "started_at": mock.ANY,
                "completed_at": mock.ANY,
                "cancel_key": "cancel-key",
            },
            method="POST",
        )

        with httpserver.wait(timeout=5) as waiting:
            redis_client.set("cancel-key", 1, ex=5)

        # check we receive the "canceled" webhook
        assert waiting.result


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


def get_local_ip_address():
    """
    Find our local IP address by opening a socket and checking where it
    connected from.
    """

    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    # we don't need a reachable destination!
    s.connect(("10.254.254.254", 1))
    ip_addr = s.getsockname()[0]
    s.close()

    return ip_addr


LOCAL_IP_ADDRESS = get_local_ip_address()
