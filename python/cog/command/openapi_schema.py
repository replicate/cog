"""
python -m cog.command.specification

This prints a JSON object describing the inputs of the model.
"""

import json
from typing import Any, Dict, List, Union

from ..errors import CogError, ConfigDoesNotExist, PredictorNotSet
from ..predictor import load_config
from ..schema import Status
from ..server.http import create_app
from ..suppress_output import suppress_output


def remove_title_next_to_ref(
    schema_node: Union[Dict[str, Any], List[Any]],
) -> Union[Dict[str, Any], List[Any]]:
    """
    Recursively remove 'title' from schema components that have a '$ref'.
    This function addresses a non-compliance issue in FastAPI's OpenAPI schema generation, where
    'title' fields adjacent to '$ref' fields can cause validation problems with some OpenAPI tools.
    """
    if isinstance(schema_node, dict):
        if "$ref" in schema_node and "title" in schema_node:
            del schema_node["title"]
        for _key, value in schema_node.items():
            remove_title_next_to_ref(value)
    elif isinstance(schema_node, list):  # type: ignore[reportUnnecessaryIsInstance]
        for i, item in enumerate(schema_node):
            schema_node[i] = remove_title_next_to_ref(item)
    return schema_node


if __name__ == "__main__":
    schema = {}
    try:
        with suppress_output():
            config = load_config()
            app = create_app(config, shutdown_event=None, is_build=True)
            if (
                app.state.setup_result
                and app.state.setup_result.status == Status.FAILED
            ):
                raise CogError(app.state.setup_result.logs)
            schema = remove_title_next_to_ref(app.openapi())
    except ConfigDoesNotExist:
        raise ConfigDoesNotExist("no cog.yaml found or present") from None
    except PredictorNotSet:
        raise PredictorNotSet("no predict method found in Predictor") from None
    print(json.dumps(schema, indent=2))
