from anthropic.types import MessageParam
from cog import BasePredictor, Input
from cog.coder import json_coder  # noqa: F401


class Predictor(BasePredictor):
    def predict(
        self,
        message: MessageParam = Input(description="Messages API."),
    ) -> str:
        return "Content: " + message["content"]
