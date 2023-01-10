from datetime import datetime
import base64
import io

from PIL import Image
import pytest
import unittest.mock as mock

from .conftest import make_client, match, uses_predictor


@uses_predictor("setup")
def test_setup_is_called(client):
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == match({"status": "succeeded", "output": "bar"})


@uses_predictor("openapi_complex_input")
def test_openapi_specification(client):
    resp = client.get("/openapi.json")
    assert resp.status_code == 200

    schema = resp.json()
    assert schema["openapi"] == "3.0.2"
    assert schema["info"] == {"title": "Cog", "version": "0.1.0"}
    assert schema["paths"] == {
        "/": {
            "get": {
                "summary": "Root",
                "operationId": "root__get",
                "responses": {
                    "200": {
                        "description": "Successful Response",
                        "content": {"application/json": {"schema": mock.ANY}},
                    }
                },
            }
        },
        "/predictions": {
            "post": {
                "summary": "Predict",
                "description": "Run a single prediction on the model",
                "operationId": "predict_predictions_post",
                "parameters": [
                    {
                        "in": "header",
                        "name": "prefer",
                        "required": False,
                        "schema": {"title": "Prefer", "type": "string"},
                    }
                ],
                "requestBody": {
                    "content": {
                        "application/json": {
                            "schema": {"$ref": "#/components/schemas/PredictionRequest"}
                        }
                    }
                },
                "responses": {
                    "200": {
                        "description": "Successful Response",
                        "content": {
                            "application/json": {
                                "schema": {
                                    "$ref": "#/components/schemas/PredictionResponse"
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
        },
        "/predictions/{prediction_id}/cancel": {
            "post": {
                "summary": "Cancel",
                "description": "Cancel a running prediction",
                "operationId": "cancel_predictions__prediction_id__cancel_post",
                "parameters": [
                    {
                        "in": "path",
                        "name": "prediction_id",
                        "required": True,
                        "schema": {"title": "Prediction ID", "type": "string"},
                    }
                ],
                "responses": {
                    "200": {
                        "content": {"application/json": {"schema": {}}},
                        "description": "Successful Response",
                    },
                    "422": {
                        "content": {
                            "application/json": {
                                "schema": {
                                    "$ref": "#/components/schemas/HTTPValidationError"
                                }
                            }
                        },
                        "description": "Validation Error",
                    },
                },
            }
        },
    }
    assert schema["components"]["schemas"]["Input"] == {
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
    }
    assert schema["components"]["schemas"]["Output"] == {
        "title": "Output",
        "type": "string",
    }
    assert schema["components"]["schemas"]["choices"] == {
        "title": "choices",
        "enum": ["foo", "bar"],
        "description": "An enumeration.",
        "type": "string",
    }
    assert schema["components"]["schemas"]["int_choices"] == {
        "description": "An enumeration.",
        "enum": [3, 4, 5],
        "title": "int_choices",
        "type": "integer",
    }


@uses_predictor("openapi_custom_output_type")
def test_openapi_specification_with_custom_user_defined_output_type(client):
    resp = client.get("/openapi.json")
    assert resp.status_code == 200

    schema = resp.json()
    assert schema["components"]["schemas"]["Output"] == {
        "$ref": "#/components/schemas/MyOutput",
        "title": "Output",
    }
    assert schema["components"]["schemas"]["MyOutput"] == {
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
    }


@uses_predictor("openapi_output_type")
def test_openapi_specification_with_custom_user_defined_output_type_called_output(
    client,
):
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


@uses_predictor("openapi_output_yield")
def test_openapi_specification_with_yield(client):
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


@uses_predictor("openapi_output_list")
def test_openapi_specification_with_list(client):
    resp = client.get("/openapi.json")
    assert resp.status_code == 200

    assert resp.json()["components"]["schemas"]["Output"] == {
        "title": "Output",
        "type": "array",
        "items": {
            "type": "string",
        },
    }


@uses_predictor("openapi_input_int_choices")
def test_openapi_specification_with_int_choices(client):
    resp = client.get("/openapi.json")
    assert resp.status_code == 200

    schema = resp.json()
    schemas = schema["components"]["schemas"]

    assert schemas["Input"]["properties"]["pick_a_number_any_number"] == {
        "allOf": [{"$ref": "#/components/schemas/pick_a_number_any_number"}],
        "x-order": 0,
    }
    assert schemas["pick_a_number_any_number"] == {
        "description": "An enumeration.",
        "enum": [1, 2],
        "title": "pick_a_number_any_number",
        "type": "integer",
    }


@uses_predictor("yield_strings")
def test_yielding_strings_from_generator_predictors(client):
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == match(
        {"status": "succeeded", "output": ["foo", "bar", "baz"]}
    )


@uses_predictor("yield_strings_file_input")
def test_yielding_strings_from_generator_predictors_file_input(client):
    resp = client.post(
        "/predictions",
        json={"input": {"file": "data:text/plain; charset=utf-8;base64,aGVsbG8="}},
    )
    assert resp.status_code == 200
    assert resp.json() == match(
        {
            "status": "succeeded",
            "output": ["hello foo", "hello bar", "hello baz"],
        }
    )


@uses_predictor("yield_files")
def test_yielding_files_from_generator_predictors(client):
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


@uses_predictor("yield_strings")
def test_asynchronous_prediction_endpoint(client):
    resp = client.post("/predictions", headers={"Prefer": "respond-async"})
    assert resp.status_code == 202

    assert resp.json() == match(
        {"status": "processing", "output": None, "started_at": mock.ANY}
    )
    assert resp.json()["started_at"] is not None


@uses_predictor("sleep")
def test_prediction_cancel(client):
    resp = client.post("/predictions/123/cancel")
    assert resp.status_code == 404

    resp = client.post(
        "/predictions",
        json={"id": "123", "input": {"sleep": 1}},
        headers={"Prefer": "respond-async"},
    )
    assert resp.status_code == 202

    resp = client.post("/predictions/123/cancel")
    assert resp.status_code == 200
