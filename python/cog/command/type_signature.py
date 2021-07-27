"""
python -m cog.command.specification

This prints a JSON object describing the inputs of the model.
"""
import json
from ..errors import ConfigDoesNotExist, ModelNotSet

from ..model import load_model

if __name__ == "__main__":
    obj = {}
    try:
        model = load_model()
    except (ConfigDoesNotExist, ModelNotSet):
        # If there is no cog.yaml or 'model' has not been set, then there is no type signature.
        # Not an error, there just isn't anything.
        pass
    else:
        obj = model.get_type_signature()
    print(json.dumps(obj, indent=2))
