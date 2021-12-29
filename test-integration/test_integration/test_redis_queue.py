import base64
import json
import multiprocessing
from contextlib import contextmanager
import pytest
from pathlib import Path

import flask
import redis
from flask import Flask, Response, jsonify
import subprocess

from .util import (
    docker_run,
    find_free_port,
    get_bridge_ip,
    get_local_ip,
    random_string,
    wait_for_port,
)


def test_queue_worker_yielding(docker_image, redis_port, request):
    project_dir = Path(__file__).parent / "fixtures/yielding-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    input_queue = multiprocessing.Queue()
    output_queue = multiprocessing.Queue()
    controller_port = find_free_port()
    local_ip = get_local_ip()
    upload_url = f"http://{local_ip}:{controller_port}/upload"
    redis_host = local_ip
    worker_name = "test-worker"
    predict_queue_name = "predict-queue"
    response_queue_name = "response-queue"

    redis_client = redis.Redis(host=redis_host, port=redis_port, db=0)

    with queue_controller(
        input_queue, output_queue, controller_port, request
    ), docker_run(
        image=docker_image,
        interactive=True,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            redis_host,
            str(redis_port),
            predict_queue_name,
            upload_url,
            worker_name,
            "model_id",
            "logs",
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname=predict_queue_name, name=predict_queue_name, id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "text": {"value": "bar"},
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"value": "foo", "status": "processing"}

        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"value": "bar", "status": "processing"}

        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"value": "baz", "status": "success"}

        response = redis_client.rpop(response_queue_name)
        assert response == None


def test_queue_worker_error(docker_image, redis_port, request):
    project_dir = Path(__file__).parent / "fixtures/failing-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    input_queue = multiprocessing.Queue()
    output_queue = multiprocessing.Queue()
    controller_port = find_free_port()
    local_ip = get_local_ip()
    upload_url = f"http://{local_ip}:{controller_port}/upload"
    redis_host = local_ip
    worker_name = "test-worker"
    predict_queue_name = "predict-queue"
    response_queue_name = "response-queue"

    redis_client = redis.Redis(host=redis_host, port=redis_port, db=0)

    with queue_controller(
        input_queue, output_queue, controller_port, request
    ), docker_run(
        image=docker_image,
        interactive=True,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            redis_host,
            str(redis_port),
            predict_queue_name,
            upload_url,
            worker_name,
            "model_id",
            "logs",
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname=predict_queue_name, name=predict_queue_name, id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "text": {"value": "bar"},
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"status": "failed", "error": "over budget"}

        response = redis_client.rpop(response_queue_name)
        assert response == None


def test_queue_worker_simple(project_dir, docker_image, docker_network, redis_port):
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    worker_name = "test-worker"
    predict_queue_name = "predict-queue"
    response_queue_name = "response-queue"

    redis_client = redis.Redis(host="localhost", port=redis_port, db=0)

    with docker_run(
        image=docker_image,
        name="worker",
        interactive=True,
        network=docker_network,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            "redis",
            "6379",
            predict_queue_name,
            worker_name,
            "model_id",
            "logs",
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname=predict_queue_name, name=predict_queue_name, id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "text": "bar",
                            "path": "data:text/plain;base64,"
                            + base64.b64encode(b"test").decode("utf-8"),
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )
        message = redis_client.brpop(response_queue_name, timeout=10)
        response = json.loads(message[1])["output"]
        assert response == "foobartest"

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
            "processing bar",
            "successfully processed bar",
            "WARNING:root:writing log message",
            "writing from C",
            "processing baz",
            "successfully processed file baz",
        ]


def test_queue_worker_file_output(docker_image, redis_port, request):
    project_dir = Path(__file__).parent / "fixtures/file-output-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    input_queue = multiprocessing.Queue()
    output_queue = multiprocessing.Queue()
    controller_port = find_free_port()
    local_ip = get_local_ip()
    upload_url = f"http://{local_ip}:{controller_port}/upload"
    redis_host = local_ip
    worker_name = "test-worker"
    predict_queue_name = "predict-queue"
    response_queue_name = "response-queue"

    redis_client = redis.Redis(host=redis_host, port=redis_port, db=0)

    with queue_controller(
        input_queue, output_queue, controller_port, request
    ), docker_run(
        image=docker_image,
        interactive=True,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            redis_host,
            str(redis_port),
            predict_queue_name,
            upload_url,
            worker_name,
            "model_id",
            "logs",
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname=predict_queue_name, name=predict_queue_name, id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {},
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )
        response_contents = output_queue.get()
        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {}


def test_queue_worker_timeout(docker_image, redis_port, request):
    project_dir = Path(__file__).parent / "fixtures/timeout-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    input_queue = multiprocessing.Queue()
    output_queue = multiprocessing.Queue()
    controller_port = find_free_port()
    local_ip = get_local_ip()
    upload_url = f"http://{local_ip}:{controller_port}/upload"
    redis_host = local_ip
    worker_name = "test-worker"
    predict_queue_name = "predict-queue"
    response_queue_name = "response-queue"

    redis_client = redis.Redis(host=redis_host, port=redis_port, db=0)

    with queue_controller(
        input_queue, output_queue, controller_port, request
    ), docker_run(
        image=docker_image,
        interactive=True,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            redis_host,
            str(redis_port),
            predict_queue_name,
            upload_url,
            worker_name,
            "model_id",
            "logs",
            "1",  # timeout
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname=predict_queue_name, name=predict_queue_name, id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 0.5},
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"status": "success", "value": "it worked!"}

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 5.0},
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"status": "failed", "error": "Prediction timed out"}


def test_queue_worker_yielding_timeout(docker_image, redis_port, request):
    project_dir = Path(__file__).parent / "fixtures/yielding-timeout-project"
    subprocess.run(["cog", "build", "-t", docker_image], check=True, cwd=project_dir)

    input_queue = multiprocessing.Queue()
    output_queue = multiprocessing.Queue()
    controller_port = find_free_port()
    local_ip = get_local_ip()
    upload_url = f"http://{local_ip}:{controller_port}/upload"
    redis_host = local_ip
    worker_name = "test-worker"
    predict_queue_name = "predict-queue"
    response_queue_name = "response-queue"

    redis_client = redis.Redis(host=redis_host, port=redis_port, db=0)

    with queue_controller(
        input_queue, output_queue, controller_port, request
    ), docker_run(
        image=docker_image,
        interactive=True,
        command=[
            "python",
            "-m",
            "cog.server.redis_queue",
            redis_host,
            str(redis_port),
            predict_queue_name,
            upload_url,
            worker_name,
            "model_id",
            "logs",
            "1",  # timeout
        ],
    ):
        redis_client.xgroup_create(
            mkstream=True, groupname=predict_queue_name, name=predict_queue_name, id="$"
        )

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 0.5},
                            "n_iterations": {"value": 1},
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )

        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"status": "success", "value": "yield 0"}

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "sleep_time": {"value": 0.7},
                            "n_iterations": {"value": 10},
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )

        # TODO(andreas): revisit this test design if it starts being flakey
        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"value": "yield 0", "status": "processing"}

        response = json.loads(redis_client.brpop(response_queue_name, timeout=10)[1])
        assert response == {"status": "failed", "error": "Prediction timed out"}
