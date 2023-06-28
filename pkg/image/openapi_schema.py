import ast

import json
import sys
from pathlib import Path

BASE_SCHEMA = '{"components":{"schemas":{"HTTPValidationError":{"properties":{"detail":{"items":{"$ref":"#/components/schemas/ValidationError"},"title":"Detail","type":"array"}},"title":"HTTPValidationError","type":"object"},"PredictionRequest":{"properties":{"created_at":{"format":"date-time","title":"Created At","type":"string"},"id":{"title":"Id","type":"string"},"input":{"$ref":"#/components/schemas/Input"},"output_file_prefix":{"title":"Output File Prefix","type":"string"},"webhook":{"format":"uri","maxLength":65536,"minLength":1,"title":"Webhook","type":"string"},"webhook_events_filter":{"default":["completed","logs","output","start"],"items":{"$ref":"#/components/schemas/WebhookEvent"},"type":"array","uniqueItems":true}},"title":"PredictionRequest","type":"object"},"PredictionResponse":{"properties":{"completed_at":{"format":"date-time","title":"Completed At","type":"string"},"created_at":{"format":"date-time","title":"Created At","type":"string"},"error":{"title":"Error","type":"string"},"id":{"title":"Id","type":"string"},"input":{"$ref":"#/components/schemas/Input"},"logs":{"default":"","title":"Logs","type":"string"},"metrics":{"title":"Metrics","type":"object"},"output":{"$ref":"#/components/schemas/Output"},"started_at":{"format":"date-time","title":"Started At","type":"string"},"status":{"$ref":"#/components/schemas/Status"},"version":{"title":"Version","type":"string"}},"title":"PredictionResponse","type":"object"},"Status":{"description":"An enumeration.","enum":["starting","processing","succeeded","canceled","failed"],"title":"Status","type":"string"},"ValidationError":{"properties":{"loc":{"items":{"anyOf":[{"type":"string"},{"type":"integer"}]},"title":"Location","type":"array"},"msg":{"title":"Message","type":"string"},"type":{"title":"Error Type","type":"string"}},"required":["loc","msg","type"],"title":"ValidationError","type":"object"},"WebhookEvent":{"description":"An enumeration.","enum":["start","output","logs","completed"],"title":"WebhookEvent","type":"string"}}},"info":{"title":"Cog","version":"0.1.0"},"openapi":"3.0.2","paths":{"/":{"get":{"operationId":"root__get","responses":{"200":{"content":{"application/json":{"schema":{"title":"Response Root  Get"}}},"description":"Successful Response"}},"summary":"Root"}},"/health-check":{"get":{"operationId":"healthcheck_health_check_get","responses":{"200":{"content":{"application/json":{"schema":{"title":"Response Healthcheck Health Check Get"}}},"description":"Successful Response"}},"summary":"Healthcheck"}},"/predictions":{"post":{"description":"Run a single prediction on the model","operationId":"predict_predictions_post","parameters":[{"in":"header","name":"prefer","required":false,"schema":{"title":"Prefer","type":"string"}}],"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PredictionRequest"}}}},"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PredictionResponse"}}},"description":"Successful Response"},"422":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/HTTPValidationError"}}},"description":"Validation Error"}},"summary":"Predict"}},"/predictions/{prediction_id}":{"put":{"description":"Run a single prediction on the model (idempotent creation).","operationId":"predict_idempotent_predictions__prediction_id__put","parameters":[{"in":"path","name":"prediction_id","required":true,"schema":{"title":"Prediction ID","type":"string"}},{"in":"header","name":"prefer","required":false,"schema":{"title":"Prefer","type":"string"}}],"requestBody":{"content":{"application/json":{"schema":{"allOf":[{"$ref":"#/components/schemas/PredictionRequest"}],"title":"Prediction Request"}}},"required":true},"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PredictionResponse"}}},"description":"Successful Response"},"422":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/HTTPValidationError"}}},"description":"Validation Error"}},"summary":"Predict Idempotent"}},"/predictions/{prediction_id}/cancel":{"post":{"description":"Cancel a running prediction","operationId":"cancel_predictions__prediction_id__cancel_post","parameters":[{"in":"path","name":"prediction_id","required":true,"schema":{"title":"Prediction ID","type":"string"}}],"responses":{"200":{"content":{"application/json":{"schema":{"title":"Response Cancel Predictions  Prediction Id  Cancel Post"}}},"description":"Successful Response"},"422":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/HTTPValidationError"}}},"description":"Validation Error"}},"summary":"Cancel"}},"/shutdown":{"post":{"operationId":"start_shutdown_shutdown_post","responses":{"200":{"content":{"application/json":{"schema":{"title":"Response Start Shutdown Shutdown Post"}}},"description":"Successful Response"}},"summary":"Start Shutdown"}}}}'
OPENAPI_TYPES = {
    "str": "string",  # includes dates, files
    "int": "integer",
    "float": "number",
    "bool": "boolean",
}


def get_value(node: ast.AST) -> "int | float | complex | str | list":
    """Return the value of constant or list of constants"""
    if isinstance(node, ast.Constant):
        return node.value
    if isinstance(node, (ast.List, ast.Tuple)):
        return [get_value(e) for e in node.elts]
    raise ValueError("Unexpected node type", type(node))


def get_annotation(node: "ast.AST | None") -> str:
    """Return the annotation as a string"""
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Constant):
        return node.value  # e.g. arg: "Path"
    # ignore Subscript (Optional[str]), BinOp (str | int), and stuff like that
    raise ValueError("Unexpected annotation type", type(node))


def get_call_name(call: ast.Call) -> str:
    """Try to get the name of a Call"""
    if isinstance(call.func, ast.Name):
        return call.func.id
    if isinstance(call.func, ast.Attribute):
        return call.func.attr
    raise ValueError("Unexpected node type", type(call), ast.unparse(call))


def find(obj: ast.AST, name: str) -> ast.AST:
    """Find a particular named node in a tree"""
    return next(node for node in ast.walk(obj) if getattr(node, "name", "") == name)


def parse_args(code: str) -> "list[tuple[ast.arg, ast.expr | ellipsis]]":
    """Parse argument, default pairs from a file with a predict function"""
    tree = ast.parse(code)
    predict = find(tree, "predict")
    assert isinstance(predict, ast.FunctionDef)
    args = predict.args.args
    # use Ellipsis instead of None here to distinguish a default of None
    defaults = [...] * (len(args) - len(predict.args.defaults)) + predict.args.defaults
    return list(zip(args, defaults))


def extract_info(code: str) -> dict:
    """Parse the schemas from a file with a predict function"""
    inputs = {"title": "Input", "type": "object", "properties": {}, "required": []}
    schemas: dict[str, dict] = {}
    for arg, default in parse_args(code):
        if arg.arg == "self":
            continue
        if isinstance(default, ast.Call) and get_call_name(default) == "Input":
            kws = {kw.arg: get_value(kw.value) for kw in default.keywords}
        elif isinstance(default, (ast.Constant, ast.List, ast.Tuple)):
            kws = {"default": get_value(default)}  # could be None
        elif default == ...:  # no default
            kws = {}
        else:
            raise ValueError("Unexpected default value", default)
        input = {"x-order": len(inputs["properties"])}
        arg_type = OPENAPI_TYPES.get(get_annotation(arg.annotation), "string")
        if get_annotation(arg.annotation) in ("Path", "File"):
            input["format"] = "uri"
        # need to handle other types
        attrs = ("description", "default", "ge", "le", "max_length", "min_length")
        for attr in attrs:
            if attr in kws:
                input[attr] = kws[attr]
        if "default" not in input:
            inputs["required"].append(arg.arg)
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
        inputs["properties"][arg.arg] = input

    output = {
        "title": "Output",
        "type": "array",
        "items": {"type": "string", "format": "uri"},
    }
    schema = json.loads(BASE_SCHEMA)
    schema["components"]["schemas"].update(Input=inputs, Output=output, **schemas)
    return schema


def extract_file(fname: "str | Path") -> dict:
    return extract_info(open(fname).read())


if __name__ == "__main__":
    if len(sys.argv) > 1:
        p = Path(sys.argv[1])
        if p.exists():
            print(json.dumps(extract_file(p)))
    else:
        print(json.dumps(extract_info(sys.stdin.read())))
