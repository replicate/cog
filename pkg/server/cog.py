import functools
import traceback
from abc import ABC, abstractmethod
from pathlib import Path

from flask import Flask, send_file, request, jsonify, abort

_VALID_INPUT_TYPES = frozenset([str])


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
            kwargs = request.form

            input_keys = set(kwargs.keys())
            expected_keys = set(self.run._args.keys())
            if input_keys != expected_keys:
                return abort(
                    400, "Expected arguments: {}".format(", ".join(expected_keys))
                )

            result = self.run(**kwargs)
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


def input(name, type):
    if type not in _VALID_INPUT_TYPES:
        type_name = _type_name(type)
        type_list = ", ".join([_type_name(t) for t in _VALID_INPUT_TYPES])
        raise ValueError(
            f"{type_name} is not a valid input type. Valid types are: {type_list}"
        )

    def wrapper(f):
        if not hasattr(f, "_args"):
            f._args = {}

        if name in f._args:
            raise ValueError(f"{name} is already defined as an argument")

        f._args[name] = type

        @functools.wraps(f)
        def wraps(self, **kwargs):
            if not isinstance(self, Model):
                raise TypeError("{self} is not an instance of cog.Model")
            return f(self, **kwargs)

        return wraps

    return wrapper


def _type_name(type) -> str:
    type_name = type
    if hasattr(type_name, "__name__"):
        type_name = type.__name__
    return type_name
