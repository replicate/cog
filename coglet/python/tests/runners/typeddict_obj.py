from typing import TypedDict

from cog import BasePredictor
from cog.coder import json_coder  # noqa: F401


class Message(TypedDict):
    message: str


class Predictor(BasePredictor):
    test_inputs = {'json': Message(message='foo')}

    def predict(self, json: Message) -> Message:
        msg = json.get('message')
        if msg is not None:
            json['message'] = f'*{msg}*'
        return json
