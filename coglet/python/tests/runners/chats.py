from openai.types.chat import ChatCompletionMessage

from cog import BasePredictor
from cog.coder import pydantic_coder  # noqa: F401


class Predictor(BasePredictor):
    test_inputs = {'msgs': [ChatCompletionMessage(role='assistant', content='foo')]}

    def predict(self, msgs: list[ChatCompletionMessage]) -> list[ChatCompletionMessage]:
        for msg in msgs:
            if msg.content is not None:
                msg.content = f'*{msg.content}*'
        return msgs
