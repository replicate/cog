import cog
import io


class Predictor(cog.Predictor):
    def predict(self) -> cog.File:
        return io.BytesIO(b"file content")
