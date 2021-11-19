"""
python -m cog.command.specification

This prints a JSON object describing the inputs of the model.
"""
import json
import sys

from ..errors import ConfigDoesNotExist, PredictorNotSet
from ..predictor import load_predictor
from ..stdout_redirector import stdout_redirector

if __name__ == "__main__":
    obj = {}
    try:
        with stdout_redirector(sys.stderr):
            predictor = load_predictor()
    except (ConfigDoesNotExist, PredictorNotSet):
        # If there is no cog.yaml or 'predict' has not been set, then there is no type signature.
        # Not an error, there just isn't anything.
        pass
    else:
        obj = predictor.get_type_signature()
    print(json.dumps(obj, indent=2))
