from openai.types.chat import ChatCompletionMessage

from cog import BasePredictor
from cog.coder import pydantic_coder  # noqa: F401


class Predictor(BasePredictor):
    test_inputs = {'msg': ChatCompletionMessage(role='assistant', content='foo')}

    def predict(self, msg: ChatCompletionMessage) -> ChatCompletionMessage:
        if msg.content is not None:
            msg.content = f'*{msg.content}*'
        return msg
