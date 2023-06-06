"""
python -m cog.command.specification

This prints a JSON object describing the inputs of the model.
"""
import json

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
    except (ConfigDoesNotExist, PredictorNotSet):
        # If there is no cog.yaml or 'predict' has not been set, then there is no type signature.
        # Not an error, there just isn't anything.
        pass
    print(json.dumps(schema, indent=2))
