import base64
import io
import os
import tempfile
from typing import Generator
from unittest import mock

from fastapi.testclient import TestClient
from PIL import Image
import pytest

import cog
from cog import Input, File, Path

from cog.server.http import create_app


def make_client(predictor: cog.Predictor, **kwargs) -> TestClient:
    app = create_app(predictor)
    with TestClient(app, **kwargs) as client:
        return client


def test_setup_is_called():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "bar"

        def predict(self) -> str:
            return self.foo

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == {"status": "success", "output": "bar"}


def test_openapi_specification():
    class Predictor(cog.Predictor):
        def predict(
            self,
            no_default: str,
            default_without_input: str = "default",
            input_with_default: int = Input(title="Some number", default=10),
            path: Path = Input(title="Some path"),
            image: File = Input(title="Some path"),
            choices: str = Input(choices=["foo", "bar"]),
        ) -> str:
            pass

    client = make_client(Predictor())
    resp = client.get("/openapi.json")
    assert resp.status_code == 200
    print(resp.json())
    assert resp.json() == {
        "openapi": "3.0.2",
        "info": {"title": "Cog", "version": "0.1.0"},
        "paths": {
            "/": {
                "get": {
                    "summary": "Root",
                    "operationId": "root__get",
                    "responses": {
                        "200": {
                            "description": "Successful Response",
                            "content": {"application/json": {"schema": {}}},
                        }
                    },
                }
            },
            "/predictions": {
                "post": {
                    "summary": "Predict",
                    "operationId": "predict_predictions_post",
                    "requestBody": {
                        "content": {
                            "application/json": {
                                "schema": {"$ref": "#/components/schemas/Request"}
                            }
                        }
                    },
                    "responses": {
                        "200": {
                            "description": "Successful Response",
                            "content": {
                                "application/json": {
                                    "schema": {"$ref": "#/components/schemas/Response"}
                                }
                            },
                        },
                        "422": {
                            "description": "Validation Error",
                            "content": {
                                "application/json": {
                                    "schema": {
                                        "$ref": "#/components/schemas/HTTPValidationError"
                                    }
                                }
                            },
                        },
                    },
                }
            },
        },
        "components": {
            "schemas": {
                "HTTPValidationError": {
                    "title": "HTTPValidationError",
                    "type": "object",
                    "properties": {
                        "detail": {
                            "title": "Detail",
                            "type": "array",
                            "items": {"$ref": "#/components/schemas/ValidationError"},
                        }
                    },
                },
                "Input": {
                    "title": "Input",
                    "required": ["no_default", "path", "image", "choices"],
                    "type": "object",
                    "properties": {
                        "no_default": {
                            "title": "No Default",
                            "type": "string",
                            "x-order": 0,
                        },
                        "default_without_input": {
                            "title": "Default Without Input",
                            "type": "string",
                            "default": "default",
                            "x-order": 1,
                        },
                        "input_with_default": {
                            "title": "Some number",
                            "type": "integer",
                            "default": 10,
                            "x-order": 2,
                        },
                        "path": {
                            "title": "Some path",
                            "type": "string",
                            "format": "uri",
                            "x-order": 3,
                        },
                        "image": {
                            "title": "Some path",
                            "type": "string",
                            "format": "uri",
                            "x-order": 4,
                        },
                        "choices": {"$ref": "#/components/schemas/choices"},
                    },
                },
                "Request": {
                    "title": "Request",
                    "type": "object",
                    "properties": {
                        "input": {"$ref": "#/components/schemas/Input"},
                        "output_file_prefix": {
                            "title": "Output File Prefix",
                            "type": "string",
                        },
                    },
                },
                "Response": {
                    "title": "Response",
                    "required": ["status"],
                    "type": "object",
                    "properties": {
                        "status": {"$ref": "#/components/schemas/Status"},
                        "output": {"title": "Output", "type": "string"},
                        "error": {"title": "Error", "type": "string"},
                    },
                    "description": "The status of a prediction.",
                },
                "Status": {
                    "title": "Status",
                    "enum": ["processing", "success", "failed"],
                    "description": "An enumeration.",
                },
                "ValidationError": {
                    "title": "ValidationError",
                    "required": ["loc", "msg", "type"],
                    "type": "object",
                    "properties": {
                        "loc": {
                            "title": "Location",
                            "type": "array",
                            "items": {"type": "string"},
                        },
                        "msg": {"title": "Message", "type": "string"},
                        "type": {"title": "Error Type", "type": "string"},
                    },
                },
                "choices": {
                    "title": "choices",
                    "enum": ["foo", "bar"],
                    "description": "An enumeration.",
                },
            }
        },
    }


def test_yielding_strings_from_generator_predictors():
    class Predictor(cog.Predictor):
        def predict(self) -> Generator[str, None, None]:
            predictions = ["foo", "bar", "baz"]
            for prediction in predictions:
                yield prediction

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == {"status": "success", "output": "baz"}


def test_yielding_files_from_generator_predictors():
    class Predictor(cog.Predictor):
        def predict(self) -> Generator[cog.Path, None, None]:
            colors = ["red", "blue", "yellow"]
            for i, color in enumerate(colors):
                temp_dir = tempfile.mkdtemp()
                temp_path = os.path.join(temp_dir, f"prediction-{i}.bmp")
                img = Image.new("RGB", (255, 255), color)
                img.save(temp_path)
                yield Path(temp_path)

    client = make_client(Predictor())
    resp = client.post("/predictions")

    assert resp.status_code == 200
    header, b64data = resp.json()["output"].split(",", 1)
    image = Image.open(io.BytesIO(base64.b64decode(b64data)))
    image_color = Image.Image.getcolors(image)[0][1]
    assert image_color == (255, 255, 0)  # yellow


# TODO: timing
@pytest.mark.skip
@mock.patch("time.time", return_value=0.0)
def test_timing(time_mock):
    class Predictor(cog.Predictor):
        def setup(self):
            time_mock.return_value = 1.0

        def predict(self):
            time_mock.return_value = 3.0
            return ""

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert float(resp.headers["X-Setup-Time"]) == 1.0
    assert float(resp.headers["X-Run-Time"]) == 2.0
