from cog import BasePredictor, File


class Predictor(BasePredictor):
    def predict(self, file: File) -> str:
        content = file.read()
        if isinstance(content, bytes):
            # Decode bytes to str assuming UTF-8 encoding; adjust if needed
            content = content.decode('utf-8')
        return content
