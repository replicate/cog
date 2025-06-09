from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def setup(self):
        self.prefix = "hello"

    def predict(
        self,
        text: str | None = Input(
            description="Text to prefix with 'hello '", default=None
        ),
    ) -> str:
        return self.prefix + " " + text
