from cog import BasePredictor, File


class Predictor(BasePredictor):
    def predict(self, file: File) -> str:
        return file.read()
