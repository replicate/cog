from pathlib import Path
import sys
import traceback

from flask import Flask, send_file, request, jsonify, Response

from ..input import (
    validate_and_convert_inputs,
    InputValidationError,
    get_type_name,
    UNSPECIFIED,
)
from ..json import to_json
from ..predictor import Predictor, run_prediction, load_predictor


class AIPlatformPredictionServer:
    def __init__(self, predictor: Predictor):
        sys.stderr.write(
            "WARNING: AIPlatformPredictionServer is experimental, do not use this in production\n"
        )
        self.predictor = predictor

    def make_app(self) -> Flask:
        self.predictor.setup()
        app = Flask(__name__)

        @app.route("/infer", methods=["POST"])
        def handle_request():
            cleanup_functions = []
            try:
                content = request.json
                instances = content["instances"]
                results = []
                for instance in instances:
                    try:
                        validate_and_convert_inputs(
                            self.predictor, instance, cleanup_functions
                        )
                    except InputValidationError as e:
                        return jsonify({"error": str(e)})
                    results.append(
                        run_prediction(self.predictor, instance, cleanup_functions)
                    )
                return Response(
                    to_json(
                        {
                            "predictions": results,
                        }
                    ),
                    mimetype="application/json",
                )
            except Exception as e:
                tb = traceback.format_exc()
                return jsonify(
                    {
                        "error": tb,
                    }
                )
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

    def start_server(self):
        app = self.make_app()
        app.run(host="0.0.0.0", port=5000)

    def create_response(self, result, setup_time, run_time):
        if isinstance(result, Path):
            resp = send_file(str(result))
        elif isinstance(result, str):
            resp = Response(result)
        else:
            resp = jsonify(result)
        resp.headers["X-Setup-Time"] = setup_time
        resp.headers["X-Run-Time"] = run_time
        return resp


if __name__ == "__main__":
    predictor = load_predictor()
    server = AIPlatformPredictionServer(predictor)
    server.start_server()
