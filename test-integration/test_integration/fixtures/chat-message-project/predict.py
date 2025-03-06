from cog import BasePredictor, CommonChatSchemaChatMessage


class Predictor(BasePredictor):

    def predict(self, messages: list[CommonChatSchemaChatMessage]) -> str:
        print(messages)
        return f"HELLO {messages[0]['role']}"
