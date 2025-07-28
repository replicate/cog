from anthropic.types import MessageParam
from cog import BasePredictor, Input
from cog.coder import json_coder  # noqa: F401


class Predictor(BasePredictor):
    test_inputs = {"messages": [{"content": "hello world"}]}

    def predict(
        self,
        messages: list[MessageParam] = Input(description="Messages API."),
    ) -> str:
        return "Content: " + "-".join([x["content"] for x in messages])
