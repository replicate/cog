import time
import sys
from contextlib import contextmanager
import os
import shutil
import tempfile
from dataclasses import dataclass
import inspect
import functools
import traceback
from abc import ABC, abstractmethod
from pathlib import Path
from typing import Optional, Any, Type, List, Callable, Dict

from flask import Flask, send_file, request, jsonify, abort, Response
from werkzeug.datastructures import FileStorage

# TODO(andreas): handle directory input
_VALID_INPUT_TYPES = frozenset([str, int, float, bool, Path])
_UNSPECIFIED = object()


class InputValidationError(Exception):
    pass


class Model(ABC):
    app: Flask

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

    def make_app(self) -> Flask:
        start_time = time.time()
        self.setup()
        app = Flask(__name__)
        setup_time = time.time() - start_time

        @app.route("/infer", methods=["POST"])
        def handle_request():
            start_time = time.time()

            cleanup_functions = []
            try:
                raw_inputs = {}
                for key, val in request.form.items():
                    raw_inputs[key] = val
                for key, val in request.files.items():
                    if key in raw_inputs:
                        return abort(
                            400, f"Duplicated argument name in form and files: {key}"
                        )
                    raw_inputs[key] = val

                if hasattr(self.run, "_inputs"):
                    try:
                        inputs = self.validate_and_convert_inputs(
                            raw_inputs, cleanup_functions
                        )
                    except InputValidationError as e:
                        return abort(400, str(e))
                else:
                    inputs = raw_inputs

                result = self.run(**inputs)
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

        @app.route("/help")
        def help():
            args = {}
            if hasattr(self.run, "_inputs"):
                input_specs = self.run._inputs
                for name, spec in input_specs.items():
                    arg = {
                        "type": _type_name(spec.type),
                    }
                    if spec.help:
                        arg["help"] = spec.help
                    if spec.default is not _UNSPECIFIED:
                        arg["default"] = str(spec.default)  # TODO: don't string this
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

    def validate_and_convert_inputs(
        self, raw_inputs: Dict[str, Any], cleanup_functions: List[Callable]
    ) -> Dict[str, Any]:
        input_specs = self.run._inputs
        inputs = {}

        for name, input_spec in input_specs.items():
            if name in raw_inputs:
                val = raw_inputs[name]

                if input_spec.type == Path:
                    if not isinstance(val, FileStorage):
                        raise InputValidationError(
                            f"Could not convert file input {name} to {_type_name(input_spec.type)}",
                        )
                    if val.filename is None:
                        raise InputValidationError(
                            f"No filename is provided for file input {name}"
                        )

                    temp_dir = tempfile.mkdtemp()
                    cleanup_functions.append(lambda: shutil.rmtree(temp_dir))

                    temp_path = os.path.join(temp_dir, val.filename)
                    with open(temp_path, "wb") as f:
                        f.write(val.stream.read())
                    converted = Path(temp_path)

                elif input_spec.type == int:
                    try:
                        converted = int(val)
                    except ValueError:
                        raise InputValidationError(
                            f"Could not convert {name}={val} to int"
                        )

                elif input_spec.type == float:
                    try:
                        converted = float(val)
                    except ValueError:
                        raise InputValidationError(
                            f"Could not convert {name}={val} to float"
                        )

                elif input_spec.type == bool:
                    if val not in [True, False]:
                        raise InputValidationError(f"{name}={val} is not a boolean")

                elif input_spec.type == str:
                    if isinstance(val, FileStorage):
                        raise InputValidationError(
                            f"Could not convert file input {name} to str"
                        )
                    converted = val

                else:
                    raise TypeError(
                        f"Internal error: Input type {input_spec} is not a valid input type"
                    )

            else:
                if input_spec.default is not _UNSPECIFIED:
                    converted = input_spec.default
                else:
                    raise InputValidationError(f"Missing expected argument: {name}")
            inputs[name] = converted

        expected_keys = set(self.run._inputs.keys())
        raw_keys = set(raw_inputs.keys())
        extraneous_keys = raw_keys - expected_keys
        if extraneous_keys:
            raise InputValidationError(
                f"Extraneous input keys: {', '.join(extraneous_keys)}"
            )

        return inputs


@contextmanager
def unzip_to_tempdir(zip_path):
    with tempfile.TemporaryDirectory() as tempdir:
        shutil.unpack_archive(zip_path, tempdir, "zip")
        yield tempdir


def make_temp_path(filename):
    # TODO(andreas): cleanup
    tempdir = tempfile.mkdtemp()
    return Path(os.path.join(tempdir, filename))


@dataclass
class InputSpec:
    type: Type
    default: Any = _UNSPECIFIED
    help: Optional[str] = None


def input(name, type, default=_UNSPECIFIED, help=None):
    if type not in _VALID_INPUT_TYPES:
        type_name = _type_name(type)
        type_list = ", ".join([_type_name(t) for t in _VALID_INPUT_TYPES])
        raise ValueError(
            f"{type_name} is not a valid input type. Valid types are: {type_list}"
        )

    def wrapper(f):
        if not hasattr(f, "_inputs"):
            f._inputs = {}

        if name in f._inputs:
            raise ValueError(f"{name} is already defined as an argument")

        if type == Path and default is not _UNSPECIFIED and default is not None:
            raise TypeError("Cannot use default with Path type")

        f._inputs[name] = InputSpec(type=type, default=default, help=help)

        @functools.wraps(f)
        def wraps(self, **kwargs):
            if not isinstance(self, Model):
                raise TypeError("{self} is not an instance of cog.Model")
            return f(self, **kwargs)

        return wraps

    return wrapper


def _type_name(type: Type) -> str:
    if type == str:
        return "str"
    if type == int:
        return "int"
    if type == float:
        return "float"
    if type == bool:
        return "bool"
    if type == Path:
        return "Path"
    return str(type)


def _method_arg_names(f) -> List[str]:
    return inspect.getfullargspec(f)[0][1:]  # 0 is self
