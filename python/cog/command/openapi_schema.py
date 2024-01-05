"""
python -m cog.command.specification

This prints a JSON object describing the inputs of the model.
"""
import json
import warnings

from ..errors import ConfigDoesNotExist, PredictorNotSet
from ..predictor import load_config
from ..server.http import create_app
from ..suppress_output import suppress_output

if __name__ == "__main__":
    schema = {}
    try:
        with suppress_output():
            config = load_config()
            app = create_app(config, shutdown_event=None)
            schema = app.openapi()
    except ConfigDoesNotExist:
        warnings.warn("no cog.yaml found or present")
    except PredictorNotSet:
        warnings.warn("no predict method found in Predictor")
    print(json.dumps(schema, indent=2))
