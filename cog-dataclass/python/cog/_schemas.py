"""
Internal schema generation for OpenAPI.

This module provides functions to generate OpenAPI JSON schemas from
PredictorInfo.
"""

from dataclasses import MISSING, Field
from typing import Any, Dict

from . import _adt as adt


def to_json_input(predictor: adt.PredictorInfo) -> Dict[str, Any]:
    """Generate OpenAPI schema for predictor inputs."""
    schema: Dict[str, Any] = {
        "properties": {},
        "type": "object",
        "title": "Input",
    }
    required = []

    for name, input_field in predictor.inputs.items():
        prop: Dict[str, Any] = {"x-order": input_field.order}

        if input_field.choices is not None:
            prop["allOf"] = [{"$ref": f"#/components/schemas/{name}"}]
        else:
            prop["title"] = name.replace("_", " ").title()
            prop.update(input_field.type.json_type())

        # Determine required status and default value:
        # - name: type = Input() -> required
        # - name: type = Input(default=value) -> not required, has default
        # - name: Optional[type] = Input() -> not required, default None
        # - name: Optional[type] = Input(default=value) -> not required, has default
        # - name: list[type] = Input() -> required
        # - name: list[type] = Input(default=[...]) -> not required, has default

        if input_field.default is None:
            if input_field.type.repetition in {
                adt.Repetition.REQUIRED,
                adt.Repetition.REPEATED,
            }:
                required.append(name)
        else:
            # Extract actual default for schema
            if isinstance(input_field.default, Field):
                if input_field.default.default_factory is not MISSING:
                    actual_default = input_field.default.default_factory()
                elif input_field.default.default is not MISSING:
                    actual_default = input_field.default.default
                else:
                    actual_default = None
            else:
                actual_default = input_field.default

            if actual_default is not None:
                normalized = input_field.type.normalize(actual_default)
                prop["default"] = input_field.type.json_encode(normalized)

        # Optional types are nullable
        if input_field.type.repetition is adt.Repetition.OPTIONAL:
            prop["nullable"] = True

        # Add constraints
        if input_field.description is not None:
            prop["description"] = input_field.description
        if input_field.ge is not None:
            prop["minimum"] = input_field.ge
        if input_field.le is not None:
            prop["maximum"] = input_field.le
        if input_field.min_length is not None:
            prop["minLength"] = input_field.min_length
        if input_field.max_length is not None:
            prop["maxLength"] = input_field.max_length
        if input_field.regex is not None:
            prop["pattern"] = input_field.regex
        if input_field.deprecated is not None:
            prop["deprecated"] = input_field.deprecated

        schema["properties"][name] = prop

    if required:
        schema["required"] = required

    return schema


def to_json_enums(predictor: adt.PredictorInfo) -> Dict[str, Any]:
    """Generate OpenAPI schema for enum inputs (choices)."""
    enums = {}

    for name, input_field in predictor.inputs.items():
        if input_field.choices is None:
            continue

        enum_schema = {
            "title": name,
            "description": "An enumeration.",
            "enum": input_field.choices,
        }
        enum_schema.update(input_field.type.primitive.json_type())
        enums[name] = enum_schema

    return enums


def to_json_output(predictor: adt.PredictorInfo) -> Dict[str, Any]:
    """Generate OpenAPI schema for predictor output."""
    return predictor.output.json_type()


def to_json_schema(predictor: adt.PredictorInfo) -> Dict[str, Any]:
    """
    Generate a complete OpenAPI schema for a predictor.

    This creates the full OpenAPI specification with Input, Output,
    and enum schemas populated from the predictor info.
    """
    # Base OpenAPI schema structure
    schema: Dict[str, Any] = {
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
            "/health-check": {
                "get": {
                    "summary": "Healthcheck",
                    "operationId": "healthcheck_health_check_get",
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
                                "schema": {
                                    "$ref": "#/components/schemas/PredictionRequest"
                                }
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
                    "operationId": "cancel_predictions__prediction_id__cancel_post",
                    "parameters": [
                        {
                            "required": True,
                            "schema": {"title": "Prediction Id", "type": "string"},
                            "name": "prediction_id",
                            "in": "path",
                        }
                    ],
                    "responses": {
                        "200": {
                            "description": "Successful Response",
                            "content": {"application/json": {"schema": {}}},
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
                "PredictionRequest": {
                    "title": "PredictionRequest",
                    "type": "object",
                    "properties": {
                        "id": {"title": "Id", "type": "string"},
                        "input": {"$ref": "#/components/schemas/Input"},
                    },
                },
                "PredictionResponse": {
                    "title": "PredictionResponse",
                    "type": "object",
                    "properties": {
                        "input": {"$ref": "#/components/schemas/Input"},
                        "output": {"$ref": "#/components/schemas/Output"},
                        "id": {"title": "Id", "type": "string"},
                        "version": {"title": "Version", "type": "string"},
                        "created_at": {
                            "title": "Created At",
                            "type": "string",
                            "format": "date-time",
                        },
                        "started_at": {
                            "title": "Started At",
                            "type": "string",
                            "format": "date-time",
                        },
                        "completed_at": {
                            "title": "Completed At",
                            "type": "string",
                            "format": "date-time",
                        },
                        "status": {"title": "Status", "type": "string"},
                        "error": {"title": "Error", "type": "string"},
                        "logs": {"title": "Logs", "type": "string"},
                        "metrics": {"title": "Metrics", "type": "object"},
                    },
                },
                "Status": {
                    "title": "Status",
                    "description": "An enumeration.",
                    "enum": [
                        "starting",
                        "processing",
                        "succeeded",
                        "canceled",
                        "failed",
                    ],
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
                                "anyOf": [{"type": "string"}, {"type": "integer"}]
                            },
                        },
                        "msg": {"title": "Message", "type": "string"},
                        "type": {"title": "Error Type", "type": "string"},
                    },
                },
            }
        },
    }

    # Add Input, Output, and enum schemas
    schema["components"]["schemas"]["Input"] = to_json_input(predictor)
    schema["components"]["schemas"]["Output"] = to_json_output(predictor)
    schema["components"]["schemas"].update(to_json_enums(predictor))

    return schema
