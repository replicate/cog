import json
import redis
from contextlib import contextmanager
import multiprocessing
from flask import Flask, request, jsonify, Response

from .util import (
    random_string,
    set_model_url,
    show_version,
    push_with_log,
    find_free_port,
    docker_run,
    get_local_ip,
    wait_for_port,
)


def test_queue_worker(cog_server, project_dir, redis_port, tmpdir_factory):
    user = random_string(10)
    model_name = random_string(10)
    model_url = f"http://localhost:{cog_server.port}/{user}/{model_name}"

    set_model_url(model_url, project_dir)
    version_id = push_with_log(project_dir)
    version_data = show_version(model_url, version_id)

    input_queue = multiprocessing.Queue()
    output_queue = multiprocessing.Queue()
    controller_port = find_free_port()
    local_ip = get_local_ip()
    upload_url = f"http://{local_ip}:{controller_port}/upload"
    redis_host = local_ip
    worker_name = "test-worker"
    predict_queue_name = "predict-queue"
    response_queue_name = "response-queue"

    wait_for_port(redis_host, redis_port)

    redis_client = redis.Redis(host=redis_host, port=redis_port, db=0)

    with queue_controller(input_queue, output_queue, controller_port), docker_run(
        image=version_data["images"][0]["uri"],
        interactive=True,
        command=[
            "cog-redis-queue-worker",
            redis_host,
            str(redis_port),
            predict_queue_name,
            upload_url,
            worker_name,
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
                            "text": {"value": "bar"},
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
        assert response_contents.decode() == "foobartest"
        assert response["url"] == "uploaded.txt"


@contextmanager
def queue_controller(input_queue, output_queue, controller_port):
    controller = QueueController(input_queue, output_queue, controller_port)
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
            f = request.files["file"]
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
