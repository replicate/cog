from typing import Any

from cog import BasePredictor
from cog.coder import json_coder  # noqa: F401


class Predictor(BasePredictor):
    test_inputs = {'json': {'message': 'foo'}}

    def predict(self, json: dict[str, Any]) -> dict[str, Any]:
        msg = json.get('message')
        if msg is not None:
            json['message'] = f'*{msg}*'
        return json
