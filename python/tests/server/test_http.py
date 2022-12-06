import base64
import io
import os
import tempfile
from typing import Iterator, List
from unittest import mock

import pytest
from fastapi.testclient import TestClient
from PIL import Image
from pydantic import BaseModel

from cog import BasePredictor, File, Input, Path
from cog.server.http import create_app


def make_client(predictor: BasePredictor, **kwargs) -> TestClient:
    app = create_app(predictor)
    with TestClient(app, **kwargs) as client:
        return client


def test_setup_is_called():
    class Predictor(BasePredictor):
        def setup(self):
            self.foo = "bar"

        def predict(self) -> str:
            return self.foo

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == {"status": "succeeded", "output": "bar"}


def test_openapi_specification():
    class Predictor(BasePredictor):
        def predict(
            self,
            no_default: str,
            default_without_input: str = "default",
            input_with_default: int = Input(default=10),
            path: Path = Input(description="Some path"),
            image: File = Input(description="Some path"),
            choices: str = Input(choices=["foo", "bar"]),
            int_choices: int = Input(choices=[3, 4, 5]),
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
                    "description": "Run a single prediction on the model",
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
                    "required": [
                        "no_default",
                        "path",
                        "image",
                        "choices",
                        "int_choices",
                    ],
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
                            "title": "Input With Default",
                            "type": "integer",
                            "default": 10,
                            "x-order": 2,
                        },
                        "path": {
                            "title": "Path",
                            "description": "Some path",
                            "type": "string",
                            "format": "uri",
                            "x-order": 3,
                        },
                        "image": {
                            "title": "Image",
                            "description": "Some path",
                            "type": "string",
                            "format": "uri",
                            "x-order": 4,
                        },
                        "choices": {
                            "allOf": [{"$ref": "#/components/schemas/choices"}],
                            "x-order": 5,
                        },
                        "int_choices": {
                            "allOf": [{"$ref": "#/components/schemas/int_choices"}],
                            "x-order": 6,
                        },
                    },
                },
                "Output": {"title": "Output", "type": "string"},
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
                    "description": "The request body for a prediction",
                },
                "Response": {
                    "title": "Response",
                    "required": ["status"],
                    "type": "object",
                    "properties": {
                        "status": {"$ref": "#/components/schemas/Status"},
                        "output": {"$ref": "#/components/schemas/Output"},
                        "error": {"title": "Error", "type": "string"},
                    },
                    "description": "The response body for a prediction",
                },
                "Status": {
                    "title": "Status",
                    "enum": ["processing", "succeeded", "failed", "canceled"],
                    "description": "An enumeration.",
                    "type": "string",
                },
                "ValidationError": {
                    "title": "ValidationError",
                    "required": ["loc", "msg", "type"],
                    "type": "object",
                    "properties": {
                        "loc": {
                            "title": "Location",
                            "type": "array",
                            "items": {
                                "anyOf": [{"type": "string"}, {"type": "integer"}],
                            },
                        },
                        "msg": {"title": "Message", "type": "string"},
                        "type": {"title": "Error Type", "type": "string"},
                    },
                },
                "choices": {
                    "title": "choices",
                    "enum": ["foo", "bar"],
                    "description": "An enumeration.",
                    "type": "string",
                },
                "int_choices": {
                    "description": "An enumeration.",
                    "enum": [3, 4, 5],
                    "title": "int_choices",
                    "type": "integer",
                },
            }
        },
    }


def test_openapi_specification_with_custom_user_defined_output_type():
    # Calling this `MyOutput` to test if cog renames it to `Output` in the schema
    class MyOutput(BaseModel):
        foo_number: int = "42"
        foo_string: str = "meaning of life"

    class Predictor(BasePredictor):
        def predict(
            self,
        ) -> MyOutput:
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
                    "description": "Run a single prediction on the model",
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
                "Input": {"title": "Input", "type": "object", "properties": {}},
                "MyOutput": {
                    "title": "MyOutput",
                    "type": "object",
                    "properties": {
                        "foo_number": {
                            "title": "Foo Number",
                            "type": "integer",
                            "default": "42",
                        },
                        "foo_string": {
                            "title": "Foo String",
                            "type": "string",
                            "default": "meaning of life",
                        },
                    },
                },
                "Output": {"$ref": "#/components/schemas/MyOutput", "title": "Output"},
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
                    "description": "The request body for a prediction",
                },
                "Response": {
                    "title": "Response",
                    "required": ["status"],
                    "type": "object",
                    "properties": {
                        "status": {"$ref": "#/components/schemas/Status"},
                        "output": {"$ref": "#/components/schemas/Output"},
                        "error": {"title": "Error", "type": "string"},
                    },
                    "description": "The response body for a prediction",
                },
                "Status": {
                    "title": "Status",
                    "enum": ["processing", "succeeded", "failed", "canceled"],
                    "description": "An enumeration.",
                    "type": "string",
                },
                "ValidationError": {
                    "title": "ValidationError",
                    "required": ["loc", "msg", "type"],
                    "type": "object",
                    "properties": {
                        "loc": {
                            "title": "Location",
                            "type": "array",
                            "items": {
                                "anyOf": [{"type": "string"}, {"type": "integer"}],
                            },
                        },
                        "msg": {"title": "Message", "type": "string"},
                        "type": {"title": "Error Type", "type": "string"},
                    },
                },
            }
        },
    }


def test_openapi_specification_with_custom_user_defined_output_type_called_output():
    # An output object called `Output` needs to be special cased because pydantic tries to dedupe it with the internal `Output`
    class Output(BaseModel):
        foo_number: int = "42"
        foo_string: str = "meaning of life"

    class Predictor(BasePredictor):
        def predict(
            self,
        ) -> Output:
            pass

    client = make_client(Predictor())
    resp = client.get("/openapi.json")
    assert resp.status_code == 200

    assert resp.json()["components"]["schemas"]["Output"] == {
        "properties": {
            "foo_number": {"default": "42", "title": "Foo Number", "type": "integer"},
            "foo_string": {
                "default": "meaning of life",
                "title": "Foo String",
                "type": "string",
            },
        },
        "title": "Output",
        "type": "object",
    }


def test_openapi_specification_with_yield():
    class Predictor(BasePredictor):
        def predict(
            self,
        ) -> Iterator[str]:
            pass

    client = make_client(Predictor())
    resp = client.get("/openapi.json")
    assert resp.status_code == 200

    assert resp.json()["components"]["schemas"]["Output"] == {
        "title": "Output",
        "type": "array",
        "items": {
            "type": "string",
        },
        "x-cog-array-type": "iterator",
    }


def test_openapi_specification_with_list():
    class Predictor(BasePredictor):
        def predict(
            self,
        ) -> List[str]:
            pass

    client = make_client(Predictor())
    resp = client.get("/openapi.json")
    assert resp.status_code == 200

    assert resp.json()["components"]["schemas"]["Output"] == {
        "title": "Output",
        "type": "array",
        "items": {
            "type": "string",
        },
    }


def test_openapi_specification_with_int_choices():
    class Predictor(BasePredictor):
        def predict(self, pick_a_number_any_number: int = Input(choices=[1, 2])) -> str:
            pass

    client = make_client(Predictor())
    resp = client.get("/openapi.json")
    assert resp.status_code == 200

    props = resp.json()["components"]["schemas"]["Input"]["properties"]

    assert props["pick_a_number_any_number"]["allOf"] == [
        {"$ref": "#/components/schemas/pick_a_number_any_number"}
    ]
    assert resp.json()["components"]["schemas"]["pick_a_number_any_number"] == {
        "description": "An enumeration.",
        "enum": [1, 2],
        "title": "pick_a_number_any_number",
        "type": "integer",
    }


def test_yielding_strings_from_generator_predictors():
    class Predictor(BasePredictor):
        def predict(self) -> Iterator[str]:
            predictions = ["foo", "bar", "baz"]
            for prediction in predictions:
                yield prediction

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == {"status": "succeeded", "output": ["foo", "bar", "baz"]}


def test_yielding_strings_from_generator_predictors_file_input():
    class Predictor(BasePredictor):
        def predict(self, file: Path) -> Iterator[str]:
            with file.open() as f:
                prefix = f.read()
            predictions = ["foo", "bar", "baz"]
            for prediction in predictions:
                yield prefix + " " + prediction

    client = make_client(Predictor())
    resp = client.post(
        "/predictions",
        json={"input": {"file": "data:text/plain; charset=utf-8;base64,aGVsbG8="}},
    )
    assert resp.status_code == 200
    assert resp.json() == {
        "status": "succeeded",
        "output": ["hello foo", "hello bar", "hello baz"],
    }


def test_yielding_files_from_generator_predictors():
    class Predictor(BasePredictor):
        def predict(self) -> Iterator[Path]:
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
    output = resp.json()["output"]

    def image_color(data_url):
        header, b64data = data_url.split(",", 1)
        image = Image.open(io.BytesIO(base64.b64decode(b64data)))
        return Image.Image.getcolors(image)[0][1]

    assert image_color(output[0]) == (255, 0, 0)  # red
    assert image_color(output[1]) == (0, 0, 255)  # blue
    assert image_color(output[2]) == (255, 255, 0)  # yellow


# TODO: timing
@pytest.mark.skip
@mock.patch("time.time", return_value=0.0)
def test_timing(time_mock):
    class Predictor(BasePredictor):
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


def test_untyped_inputs():
    class Predictor(BasePredictor):
        def predict(self, input) -> str:
            return input

    with pytest.raises(TypeError):
        client = make_client(Predictor())


def test_input_with_unsupported_type():
    class Input(BaseModel):
        text: str

    class Predictor(BasePredictor):
        def predict(self, input: Input) -> str:
            return input.text

    with pytest.raises(TypeError):
        client = make_client(Predictor())
