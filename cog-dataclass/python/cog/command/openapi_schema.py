"""
python -m cog.command.openapi_schema

This prints a JSON object describing the inputs of the model.
"""

import json
from typing import Any, Dict, List, Union

from ..config import Config
from ..errors import ConfigDoesNotExist
from ..schema import Status
from ..server.http import create_app
from ..suppress_output import suppress_output


def remove_title_next_to_ref(
    schema_node: Union[Dict[str, Any], List[Any]],
) -> Union[Dict[str, Any], List[Any]]:
    """
    Recursively remove 'title' from schema components that have a '$ref'.
    This function addresses a non-compliance issue in FastAPI's OpenAPI schema generation.
    """
    if isinstance(schema_node, dict):
        if "$ref" in schema_node and "title" in schema_node:
            del schema_node["title"]
        for _key, value in schema_node.items():
            remove_title_next_to_ref(value)
    elif isinstance(schema_node, list):
        for i, item in enumerate(schema_node):
            schema_node[i] = remove_title_next_to_ref(item)
    return schema_node


def fix_nullable_anyof(schema_node: Union[Dict[str, Any], List[Any]]) -> None:
    """
    Convert anyOf with null type to nullable: true for OpenAPI 3.0 compatibility.

    FastAPI generates: {"anyOf": [{"type": "string"}, {"type": "null"}]}
    OpenAPI 3.0 wants: {"type": "string", "nullable": true}
    """
    if isinstance(schema_node, dict):
        if "anyOf" in schema_node:
            anyof = schema_node["anyOf"]
            if isinstance(anyof, list) and len(anyof) == 2:
                # Check if one is {"type": "null"}
                null_idx = None
                other_idx = None
                for i, item in enumerate(anyof):
                    if isinstance(item, dict) and item.get("type") == "null":
                        null_idx = i
                    else:
                        other_idx = i

                if null_idx is not None and other_idx is not None:
                    other = anyof[other_idx]
                    if isinstance(other, dict):
                        # Replace anyOf with the non-null type + nullable
                        del schema_node["anyOf"]
                        schema_node.update(other)
                        schema_node["nullable"] = True

        for value in schema_node.values():
            fix_nullable_anyof(value)
    elif isinstance(schema_node, list):
        for item in schema_node:
            fix_nullable_anyof(item)


if __name__ == "__main__":
    schema: Dict[str, Any] = {}
    try:
        with suppress_output():
            app = create_app(cog_config=Config(), shutdown_event=None, is_build=True)
            if (
                app.state.setup_result
                and app.state.setup_result.status == Status.FAILED
            ):
                raise Exception(app.state.setup_result.logs)
            schema = app.openapi()
            remove_title_next_to_ref(schema)
            fix_nullable_anyof(schema)
    except FileNotFoundError:
        raise ConfigDoesNotExist("no cog.yaml found or present") from None

    print(json.dumps(schema, indent=2))
