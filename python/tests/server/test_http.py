import base64
import io
import os
from pathlib import Path
import tempfile
from typing import Generator
from unittest import mock

from fastapi.testclient import TestClient
from PIL import Image
from pydantic import BaseModel, Field

import cog
from cog.server.http import create_app


def make_client(predictor: cog.Predictor) -> TestClient:
    app = create_app(predictor)
    with TestClient(app) as client:
        return client


def test_setup_is_called():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "bar"

        def predict(self) -> str:
            return self.foo

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.json() == {"status": "success", "output": "bar"}


def test_openapi_specification():
    class Predictor(cog.Predictor):
        class Input(BaseModel):
            text: str = Field(..., title="Some text")
            number: int = Field(10, title="Some number")
            path: Path = Field(..., title="Some path")

        def predict(self, input: Input) -> str:
            pass

    client = make_client(Predictor())
    resp = client.get("/openapi.json")
    assert resp.status_code == 200
    assert resp.json() == {
        "openapi": "3.0.2",
        "info": {"title": "FastAPI", "version": "0.1.0"},
        "paths": {
            "/predict": {
                "post": {
                    "summary": "Predict",
                    "operationId": "predict_predict_post",
                    "requestBody": {
                        "content": {
                            "application/json": {
                                "schema": {"$ref": "#/components/schemas/Input"}
                            }
                        },
                    },
                    "responses": {
                        "200": {
                            "description": "Successful Response",
                            "content": {
                                "application/json": {
                                    "schema": {
                                        "$ref": "#/components/schemas/CogResponse"
                                    }
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
            }
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
                    "required": ["text", "path"],
                    "type": "object",
                    "properties": {
                        "text": {"title": "Some text", "type": "string"},
                        "number": {
                            "title": "Some number",
                            "type": "integer",
                            "default": 10,
                        },
                        "path": {
                            "title": "Some path",
                            "type": "string",
                            "format": "path",
                        },
                    },
                },
                "CogResponse": {
                    "title": "CogResponse",
                    "required": ["status", "output"],
                    "type": "object",
                    "properties": {
                        "status": {"title": "Status", "type": "string"},
                        "output": {"title": "Output", "type": "string"},
                    },
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
    resp = client.post("/predict")
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
                yield cog.Path(temp_path)

    client = make_client(Predictor())
    resp = client.post("/predict")

    assert resp.status_code == 200
    header, b64data = resp.json()["output"].split(",", 1)
    image = Image.open(io.BytesIO(base64.b64decode(b64data)))
    image_color = Image.Image.getcolors(image)[0][1]
    assert image_color == (255, 255, 0)  # yellow


@mock.patch("time.time", return_value=0.0)
def test_timing(time_mock):
    class Predictor(cog.Predictor):
        def setup(self):
            time_mock.return_value = 1.0

        def predict(self):
            time_mock.return_value = 3.0
            return ""

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert float(resp.headers["X-Setup-Time"]) == 1.0
    assert float(resp.headers["X-Run-Time"]) == 2.0
