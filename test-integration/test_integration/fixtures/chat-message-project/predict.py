from cog import BasePredictor, ChatMessage


class Predictor(BasePredictor):

    def predict(self, messages: list[ChatMessage]) -> str:
        print(messages)
        return f"HELLO {messages[0]['role']}"
