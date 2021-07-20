import sys
import traceback

from flask import Flask, send_file, request, jsonify, Response

from ..input import (
    validate_and_convert_inputs,
    InputValidationError,
    get_type_name,
    UNSPECIFIED,
)
from ..model import Model, run_model


class AIPlatformPredictionServer:
    def __init__(self, model: Model):
        sys.stderr.write(
            "WARNING: AIPlatformPredictionServer is experimental, do not use this in production\n"
        )
        self.model = model

    def make_app(self) -> Flask:
        self.model.setup()
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
                            self.model, instance, cleanup_functions
                        )
                    except InputValidationError as e:
                        return jsonify({"error": str(e)})
                    results.append(run_model(self.model, instance, cleanup_functions))
                return jsonify(
                    {
                        "predictions": results,
                    }
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

        @app.route("/help")
        def help():
            args = {}
            if hasattr(self.model.predict, "_inputs"):
                input_specs = self.model.predict._inputs
                for name, spec in input_specs.items():
                    arg = {
                        "type": get_type_name(spec.type),
                    }
                    if spec.help:
                        arg["help"] = spec.help
                    if spec.default is not UNSPECIFIED:
                        arg["default"] = str(spec.default)  # TODO: don't string this
                    if spec.min is not None:
                        arg["min"] = str(spec.min)  # TODO: don't string this
                    if spec.max is not None:
                        arg["max"] = str(spec.max)  # TODO: don't string this
                    args[name] = arg
            return jsonify({"arguments": args})

        return app

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
