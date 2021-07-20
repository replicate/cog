import json
import multiprocessing
from contextlib import contextmanager
import pytest

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


def test_queue_worker(project_dir, docker_image, redis_port, request):
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
                            "path": {
                                "file": {
                                    "name": "myinput.txt",
                                    "url": f"http://{local_ip}:{controller_port}/download",
                                }
                            },
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )
        input_queue.put("test")
        response = json.loads(redis_client.brpop(response_queue_name)[1])["value"]
        assert response == "foobartest"

        predict_id = random_string(10)
        redis_client.xadd(
            name=predict_queue_name,
            fields={
                "value": json.dumps(
                    {
                        "id": predict_id,
                        "inputs": {
                            "text": {"value": "baz"},
                            "output_file": {"value": "true"},
                            "path": {
                                "file": {
                                    "name": "myinput.txt",
                                    "url": f"http://{local_ip}:{controller_port}/download",
                                }
                            },
                        },
                        "response_queue": response_queue_name,
                    }
                ),
            },
        )
        input_queue.put("test")
        response_contents = output_queue.get()
        response = json.loads(redis_client.brpop(response_queue_name)[1])["file"]
        assert response_contents.decode() == "foobaztest"
        assert response["url"] == "uploaded.txt"

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

        assert setup_log_lines == ["setting up model"]
        assert run_log_lines == [
            "processing bar",
            "successfully processed bar",
            "processing baz",
            "successfully processed file baz",
        ]


@contextmanager
def queue_controller(input_queue, output_queue, controller_port, request):
    controller = QueueController(input_queue, output_queue, controller_port)
    request.addfinalizer(controller.kill)
    controller.start()
    yield controller
    controller.kill()


class QueueController(multiprocessing.Process):
    def __init__(self, input_queue, output_queue, port):
        super().__init__()
        self.input_queue = input_queue
        self.output_queue = output_queue
        self.port = port

    def run(self):
        app = Flask("test-queue-controller")

        @app.route("/", methods=["GET"])
        def handle_index():
            return "OK"

        @app.route("/upload", methods=["PUT"])
        def handle_upload():
            f = flask.request.files["file"]
            contents = f.read()
            self.output_queue.put(contents)
            return jsonify({"url": "uploaded.txt"})

        @app.route("/download", methods=["GET"])
        def handle_download():
            contents = self.input_queue.get()
            return Response(
                contents,
                mimetype="text/plain",
                headers={"Content-disposition": "attachment; filename=myinput.txt"},
            )

        app.run(host="0.0.0.0", port=self.port, debug=False)
