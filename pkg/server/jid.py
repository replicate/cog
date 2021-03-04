import traceback
from abc import ABC, abstractmethod
from pathlib import Path

from flask import Flask, send_file, request, jsonify


class Model(ABC):
    @abstractmethod
    def setup(self):
        pass

    @abstractmethod
    def run(self, **kwargs):
        pass

    def cli_run(self):
        self.setup()
        result = self.run()
        print(result)

    def start_server(self):
        self.setup()
        app = Flask(__name__)

        @app.route("/infer", methods=["POST"])
        def handle_request():
            args = request.form
            result = self.run(**args)
            return self.create_response(result)

        @app.route("/infer-ai-platform", methods=["POST"])
        def handle_ai_platform_request():
            try:
                content = request.json
                instances = content["instances"]
                results = []
                for instance in instances:
                    results.append(self.run(**instance))
                return jsonify({"predictions": results,})
            except Exception as e:
                tb = traceback.format_exc()
                return jsonify({"error": tb,})

        @app.route("/ping")
        def ping():
            return "PONG"


        app.run(host="0.0.0.0", port=5000)

    def create_response(self, result):
        if isinstance(result, Path):
            return send_file(str(result))
        elif isinstance(result, str):
            return result
