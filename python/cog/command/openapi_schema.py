"""
python -m cog.command.specification

This prints a JSON object describing the inputs of the model.
"""

import json

from ..errors import CogError, ConfigDoesNotExist, PredictorNotSet
from ..predictor import load_config
from ..schema import Status
from ..server.http import create_app
from ..suppress_output import suppress_output

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
            schema = app.openapi()
    except ConfigDoesNotExist:
        raise ConfigDoesNotExist("no cog.yaml found or present") from None
    except PredictorNotSet:
        raise PredictorNotSet("no predict method found in Predictor") from None
    print(json.dumps(schema, indent=2))
