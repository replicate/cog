from datetime import datetime

from cog import BasePredictor

ERROR = 'invalid input field dt: unsupported Cog type datetime'


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, dt: datetime) -> str:
        pass
