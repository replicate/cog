import json
import multiprocessing
from contextlib import contextmanager

import redis
import flask
from flask import Flask, Response, jsonify

from .util import (
    docker_run,
    find_free_port,
    get_bridge_ip,
    push_with_log,
    random_string,
    set_model_url,
    show_version,
    wait_for_port
)


def test_queue_worker(cog_server, project_dir, redis_port, tmpdir_factory, request):
    user = random_string(10)
    model_name = random_string(10)
    model_url = f"http://localhost:{cog_server.port}/{user}/{model_name}"

    set_model_url(model_url, project_dir)
    version_id = push_with_log(project_dir)
    version_data = show_version(model_url, version_id)

    input_queue = multiprocessing.Queue()
    output_queue = multiprocessing.Queue()
    controller_port = find_free_port()
    bridge_ip = get_bridge_ip()
    upload_url = f"http://{bridge_ip}:{controller_port}/upload"
    redis_host = bridge_ip
    worker_name = "test-worker"
    predict_queue_name = "predict-queue"
    response_queue_name = "response-queue"

    wait_for_port(redis_host, redis_port)

    redis_client = redis.Redis(host=redis_host, port=redis_port, db=0)

    with queue_controller(input_queue, output_queue, controller_port, request), docker_run(
        image=version_data["images"][0]["uri"],
        interactive=True,
        command=[
            "cog-redis-queue-worker",
            redis_host,
            str(redis_port),
            predict_queue_name,
            upload_url,
            worker_name,
            f"{user}/{model_name}:{version_id}",
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
                                    "url": f"http://{bridge_ip}:{controller_port}/download",
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
                                    "url": f"http://{bridge_ip}:{controller_port}/download",
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
