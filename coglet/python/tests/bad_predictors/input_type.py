from cog import BasePredictor

ERROR = 'missing type annotation for input: s'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s) -> str:
        pass
