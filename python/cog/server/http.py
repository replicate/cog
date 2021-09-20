from pathlib import Path
import sys
import time
import types

from flask import Flask, send_file, request, jsonify, Response

from ..input import (
    validate_and_convert_inputs,
    InputValidationError,
)
from ..json import to_json
from ..predictor import Predictor, run_prediction, load_predictor


class HTTPServer:
    def __init__(self, predictor: Predictor):
        self.predictor = predictor

    def make_app(self) -> Flask:
        start_time = time.time()
        self.predictor.setup()
        app = Flask(__name__)
        setup_time = time.time() - start_time

        @app.route("/predict", methods=["POST"])
        @app.route("/infer", methods=["POST"])  # deprecated
        def handle_request():
            start_time = time.time()

            cleanup_functions = []
            try:
                raw_inputs = {}
                for key, val in request.form.items():
                    raw_inputs[key] = val
                for key, val in request.files.items():
                    if key in raw_inputs:
                        return _abort400(
                            f"Duplicated argument name in form and files: {key}"
                        )
                    raw_inputs[key] = val

                if hasattr(self.predictor.predict, "_inputs"):
                    try:
                        inputs = validate_and_convert_inputs(
                            self.predictor, raw_inputs, cleanup_functions
                        )
                    except InputValidationError as e:
                        return _abort400(str(e))
                else:
                    inputs = raw_inputs

                result = run_prediction(self.predictor, inputs, cleanup_functions)
                run_time = time.time() - start_time
                return self.create_response(result, setup_time, run_time)
            finally:
                for cleanup_function in cleanup_functions:
                    try:
                        cleanup_function()
                    except Exception as e:
                        sys.stderr.write(f"Cleanup function caught error: {e}")

        @app.route("/ping")
        def ping():
            return "PONG"

        @app.route("/type-signature")
        def type_signature():
            return jsonify(self.predictor.get_type_signature())

        return app

    def start_server(self):
        app = self.make_app()
        app.run(host="0.0.0.0", port=5000)

    def create_response(self, result, setup_time, run_time):
        # loop over generator function to get the last result
        if isinstance(result, types.GeneratorType):
            last_result = None
            for iteration in enumerate(result):
                last_result = iteration        
            # last result is a tuple with (index, value)
            result = last_result[1]

        if isinstance(result, Path):
            resp = send_file(str(result))
        elif isinstance(result, str):
            resp = Response(result, mimetype="text/plain")
        else:
            resp = Response(to_json(result), mimetype="application/json")
        resp.headers["X-Setup-Time"] = setup_time
        resp.headers["X-Run-Time"] = run_time
        return resp


def _abort400(message):
    resp = jsonify({"message": message})
    resp.status_code = 400
    return resp


if __name__ == "__main__":
    predictor = load_predictor()
    server = HTTPServer(predictor)
    server.start_server()
