import ast
import json
import sys
import types
import typing
from pathlib import Path

try:
    assert ast.unparse
except (AssertionError, AttributeError):
    # bad "compat" with python3.8
    ast.unparse = repr

BASE_SCHEMA = """
{
  "components": {
    "schemas": {
      "HTTPValidationError": {
        "properties": {
          "detail": {
            "items": { "$ref": "#/components/schemas/ValidationError" },
            "title": "Detail",
            "type": "array"
          }
        },
        "title": "HTTPValidationError",
        "type": "object"
      },
      "PredictionRequest": {
        "properties": {
          "created_at": {
            "format": "date-time",
            "title": "Created At",
            "type": "string"
          },
          "id": { "title": "Id", "type": "string" },
          "input": { "$ref": "#/components/schemas/Input" },
          "output_file_prefix": {
            "title": "Output File Prefix",
            "type": "string"
          },
          "webhook": {
            "format": "uri",
            "maxLength": 65536,
            "minLength": 1,
            "title": "Webhook",
            "type": "string"
          },
          "webhook_events_filter": {
            "default": ["start", "output", "logs", "completed"],
            "items": { "$ref": "#/components/schemas/WebhookEvent" },
            "type": "array"
          }
        },
        "title": "PredictionRequest",
        "type": "object"
      },
      "PredictionResponse": {
        "properties": {
          "completed_at": {
            "format": "date-time",
            "title": "Completed At",
            "type": "string"
          },
          "created_at": {
            "format": "date-time",
            "title": "Created At",
            "type": "string"
          },
          "error": { "title": "Error", "type": "string" },
          "id": { "title": "Id", "type": "string" },
          "input": { "$ref": "#/components/schemas/Input" },
          "logs": { "default": "", "title": "Logs", "type": "string" },
          "metrics": { "title": "Metrics", "type": "object" },
          "output": { "$ref": "#/components/schemas/Output" },
          "started_at": {
            "format": "date-time",
            "title": "Started At",
            "type": "string"
          },
          "status": { "$ref": "#/components/schemas/Status" },
          "version": { "title": "Version", "type": "string" }
        },
        "title": "PredictionResponse",
        "type": "object"
      },
      "Status": {
        "description": "An enumeration.",
        "enum": ["starting", "processing", "succeeded", "canceled", "failed"],
        "title": "Status",
        "type": "string"
      },
      "ValidationError": {
        "properties": {
          "loc": {
            "items": { "anyOf": [{ "type": "string" }, { "type": "integer" }] },
            "title": "Location",
            "type": "array"
          },
          "msg": { "title": "Message", "type": "string" },
          "type": { "title": "Error Type", "type": "string" }
        },
        "required": ["loc", "msg", "type"],
        "title": "ValidationError",
        "type": "object"
      },
      "WebhookEvent": {
        "description": "An enumeration.",
        "enum": ["start", "output", "logs", "completed"],
        "title": "WebhookEvent",
        "type": "string"
      }
    }
  },
  "info": { "title": "Cog", "version": "0.1.0" },
  "openapi": "3.0.2",
  "paths": {
    "/": {
      "get": {
        "operationId": "root__get",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": { "title": "Response Root  Get" }
              }
            },
            "description": "Successful Response"
          }
        },
        "summary": "Root"
      }
    },
    "/health-check": {
      "get": {
        "operationId": "healthcheck_health_check_get",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": { "title": "Response Healthcheck Health Check Get" }
              }
            },
            "description": "Successful Response"
          }
        },
        "summary": "Healthcheck"
      }
    },
    "/predictions": {
      "post": {
        "description": "Run a single prediction on the model",
        "operationId": "predict_predictions_post",
        "parameters": [
          {
            "in": "header",
            "name": "prefer",
            "required": false,
            "schema": { "title": "Prefer", "type": "string" }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/PredictionRequest" }
            }
          }
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/PredictionResponse" }
              }
            },
            "description": "Successful Response"
          },
          "422": {
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/HTTPValidationError" }
              }
            },
            "description": "Validation Error"
          }
        },
        "summary": "Predict"
      }
    },
    "/predictions/{prediction_id}": {
      "put": {
        "description": "Run a single prediction on the model (idempotent creation).",
        "operationId": "predict_idempotent_predictions__prediction_id__put",
        "parameters": [
          {
            "in": "path",
            "name": "prediction_id",
            "required": true,
            "schema": { "title": "Prediction ID", "type": "string" }
          },
          {
            "in": "header",
            "name": "prefer",
            "required": false,
            "schema": { "title": "Prefer", "type": "string" }
          }
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "allOf": [{ "$ref": "#/components/schemas/PredictionRequest" }],
                "title": "Prediction Request"
              }
            }
          },
          "required": true
        },
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/PredictionResponse" }
              }
            },
            "description": "Successful Response"
          },
          "422": {
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/HTTPValidationError" }
              }
            },
            "description": "Validation Error"
          }
        },
        "summary": "Predict Idempotent"
      }
    },
    "/predictions/{prediction_id}/cancel": {
      "post": {
        "description": "Cancel a running prediction",
        "operationId": "cancel_predictions__prediction_id__cancel_post",
        "parameters": [
          {
            "in": "path",
            "name": "prediction_id",
            "required": true,
            "schema": { "title": "Prediction ID", "type": "string" }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "title": "Response Cancel Predictions  Prediction Id  Cancel Post"
                }
              }
            },
            "description": "Successful Response"
          },
          "422": {
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/HTTPValidationError" }
              }
            },
            "description": "Validation Error"
          }
        },
        "summary": "Cancel"
      }
    },
    "/shutdown": {
      "post": {
        "operationId": "start_shutdown_shutdown_post",
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": { "title": "Response Start Shutdown Shutdown Post" }
              }
            },
            "description": "Successful Response"
          }
        },
        "summary": "Start Shutdown"
      }
    }
  }
}
"""

OPENAPI_TYPES = {
    "str": "string",  # includes dates, files
    "int": "integer",
    "float": "number",
    "bool": "boolean",
    "list": "array",
    "cog.Path": "string",
    "cog.File": "string",
    "Path": "string",
    "File": "string",
}


def find(obj: ast.AST, name: str) -> ast.AST:
    """Find a particular named node in a tree"""
    return next(node for node in ast.walk(obj) if getattr(node, "name", "") == name)


if typing.TYPE_CHECKING:
    AstVal: "typing.TypeAlias" = (
        "int | float | complex | str | list[AstVal] | bytes | None"
    )
    AstValNoBytes: "typing.TypeAlias" = "int | float | str | list[AstValNoBytes]"
    JSONObject: "typing.TypeAlias" = (
        "int | float | str | list[JSONObject] | JSONDict | None"
    )
    JSONDict: "typing.TypeAlias" = "dict[str, JSONObject]"


def to_serializable(val: "AstVal") -> "JSONObject":
    if isinstance(val, bytes):
        return val.decode("utf-8")
    elif isinstance(val, list):
        return [to_serializable(x) for x in val]
    elif isinstance(val, complex):
        msg = "complex inputs are not supported"
        raise ValueError(msg)
    else:
        return val


def get_value(node: ast.AST) -> "AstVal":
    """Return the value of constant or list of constants"""
    if isinstance(node, ast.Constant):
        return node.value
    # for python3.7, were deprecated for Constant
    if isinstance(node, (ast.Str, ast.Bytes)):
        return node.s
    if isinstance(node, ast.Num):
        return node.n
    if isinstance(node, (ast.List, ast.Tuple)):
        return [get_value(e) for e in node.elts]
    if isinstance(node, ast.UnaryOp) and isinstance(node.op, ast.USub):
        return -typing.cast(typing.Union[int, float, complex], get_value(node.operand))
    raise ValueError("Unexpected node type", type(node))


def get_annotation(node: "ast.AST | None") -> str:
    """Return the annotation as a string"""
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Constant):
        return node.value  # e.g. arg: "Path"
    # ignore Subscript (Optional[str]), BinOp (str | int), and stuff like that
    # except we may need to care about list/List[str]
    raise ValueError("Unexpected annotation type", type(node))


def get_call_name(call: ast.Call) -> str:
    """Try to get the name of a Call"""
    if isinstance(call.func, ast.Name):
        return call.func.id
    if isinstance(call.func, ast.Attribute):
        return call.func.attr
    raise ValueError("Unexpected node type", type(call), ast.unparse(call))


def parse_args(tree: ast.AST) -> "list[tuple[ast.arg, ast.expr | types.EllipsisType]]":
    """Parse argument, default pairs from a file with a predict function"""
    predict = find(tree, "predict")
    assert isinstance(predict, ast.FunctionDef)
    args = predict.args.args  # [-len(defaults) :]
    # use Ellipsis instead of None here to distinguish a default of None
    defaults = [...] * (len(args) - len(predict.args.defaults)) + predict.args.defaults
    return list(zip(args, defaults))


def parse_assignment(assignment: ast.AST) -> "None | tuple[str, JSONObject]":
    """Parse an assignment into an OpenAPI object property"""
    if isinstance(assignment, ast.AnnAssign):
        assert isinstance(assignment.target, ast.Name)  # shouldn't be an Attribute
        default = {}
        if assignment.value:
            try:
                default = {"default": to_serializable(get_value(assignment.value))}
            except UnicodeDecodeError:
                pass
        return assignment.target.id, {
            "title": assignment.target.id.replace("_", " ").title(),
            "type": OPENAPI_TYPES[get_annotation(assignment.annotation)],
            **default,
        }
    if isinstance(assignment, ast.Assign):
        if len(assignment.targets) == 1 and isinstance(assignment.targets[0], ast.Name):
            value = to_serializable(get_value(assignment.value))
            return assignment.targets[0].id, {
                "title": assignment.targets[0].id.replace("_", " ").title(),
                "type": OPENAPI_TYPES[type(value).__name__],
                "default": value,
            }
        raise ValueError("Unexpected assignment", assignment)
    return None


def parse_class(classdef: ast.AST) -> "JSONDict":
    """Parse a class definition into an OpenAPI object"""
    assert isinstance(classdef, ast.ClassDef)
    properties = {
        assignment[0]: assignment[1]
        for assignment in map(parse_assignment, classdef.body)
        if assignment
    }
    return {
        "title": classdef.name,
        "type": "object",
        "properties": properties,
    }


# The supported types are:
# str: a string
# int: an integer
# float: a floating point number
# bool: a boolean
# cog.File: a file-like object representing a file
# cog.Path: a path to a file on disk

BASE_TYPES = ["str", "int", "float", "bool", "File", "Path"]


def resolve_name(node: ast.expr) -> str:
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Index):
        # deprecated, but needed for py3.8
        return resolve_name(node.value)  # type: ignore
    if isinstance(node, ast.Attribute):
        return node.attr
    if isinstance(node, ast.Subscript):
        return resolve_name(node.value)
    raise ValueError("Unexpected node type", type(node), ast.unparse(node))


def parse_return_annotation(
    tree: ast.AST, fn: str = "predict"
) -> "tuple[JSONDict, JSONDict]":
    predict = find(tree, fn)
    if not isinstance(predict, ast.FunctionDef):
        raise ValueError("Could not find predict function")
    annotation = predict.returns
    if not annotation:
        raise TypeError(
            """You must set an output type. If your model can return multiple output types, you can explicitly set `Any` as the output type.

For example:

    from typing import Any

    def predict(
        self,
        image: Path = Input(description="Input image"),
    ) -> Any:
        ...
"""
        )
    # attributes should be resolved to names, maybe blindly
    # subscript values are iterator or
    name = resolve_name(annotation)
    if isinstance(annotation, ast.Subscript):
        # forget about other subscripts like Optional, and assume otherlib.File will still be an uri
        slice = resolve_name(annotation.slice)
        format = {"format": "uri"} if slice in ("Path", "File") else {}
        array_type = {"x-cog-array-type": "iterator"} if "Iterator" in name else {}
        display_type = (
            {"x-cog-array-display": "concatenate"} if "Concatenate" in name else {}
        )
        return {}, {
            "title": "Output",
            "type": "array",
            "items": {
                "type": OPENAPI_TYPES.get(slice, slice),
                **format,
            },
            **array_type,
            **display_type,
        }
    if name in BASE_TYPES:
        # otherwise figure this out...
        format = {"format": "uri"} if name in ("Path", "File") else {}
        return {}, {"title": "Output", "type": OPENAPI_TYPES.get(name, name), **format}
    # it must be a custom object
    schema: JSONDict = {name: parse_class(find(tree, name))}
    return schema, {
        "title": "Output",
        "$ref": f"#/components/schemas/{name}",
    }


KEPT_ATTRS = ("description", "default", "ge", "le", "max_length", "min_length", "regex")


def extract_info(code: str) -> "JSONDict":
    """Parse the schemas from a file with a predict function"""
    tree = ast.parse(code)
    properties: JSONDict = {}
    inputs: JSONDict = {"title": "Input", "type": "object", "properties": properties}
    required: list[str] = []
    schemas: JSONDict = {}
    for arg, default in parse_args(tree):
        if arg.arg == "self":
            continue
        if isinstance(default, ast.Call) and get_call_name(default) == "Input":
            kws = {}
            for kw in default.keywords:
                if kw.arg is None:
                    msg = "unknown argument for Input"
                    raise ValueError(msg)
                kws[kw.arg] = to_serializable(get_value(kw.value))
        elif isinstance(default, (ast.Constant, ast.List, ast.Tuple, ast.Str, ast.Num)):
            kws = {"default": to_serializable(get_value(default))}  # could be None
        elif default == ...:  # no default
            kws = {}
        else:
            raise ValueError("Unexpected default value", default)
        input: JSONDict = {"x-order": len(properties)}
        # need to handle other types?
        arg_type = OPENAPI_TYPES.get(get_annotation(arg.annotation), "string")
        if get_annotation(arg.annotation) in ("Path", "File"):
            input["format"] = "uri"
        for attr in KEPT_ATTRS:
            if attr in kws:
                input[attr] = kws[attr]
        if "default" not in input:
            required.append(arg.arg)
        if "choices" in kws and isinstance(kws["choices"], list):
            input["allOf"] = [{"$ref": f"#/components/schemas/{arg.arg}"}]
            # could use type(kws["choices"][0]).__name__
            schemas[arg.arg] = {
                "title": arg.arg,
                "enum": kws["choices"],
                "type": arg_type,
                "description": "An enumeration.",
            }
        else:
            input["title"] = arg.arg.replace("_", " ").title()
            input["type"] = arg_type
        properties[arg.arg] = input
    if required:
        inputs["required"] = list(required)
    # List[Path], list[Path], str, Iterator[str], MyOutput, Output
    return_schema, output = parse_return_annotation(tree, "predict")
    schema: JSONDict = json.loads(BASE_SCHEMA)
    components: JSONDict = {
        "Input": inputs,
        "Output": output,
        **schemas,
        **return_schema,
    }
    # trust me, typechecker, I know BASE_SCHEMA
    x: JSONDict = schema["components"]["schemas"]  # type: ignore
    x.update(components)
    return schema


def extract_file(fname: "str | Path") -> "JSONObject":
    return extract_info(open(fname, encoding="utf-8").read())


if __name__ == "__main__":
    if len(sys.argv) > 1:
        p = Path(sys.argv[1])
        if p.exists():
            print(json.dumps(extract_file(p)))
    else:
        print(json.dumps(extract_info(sys.stdin.read())))
