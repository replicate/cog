from typing import Optional

from cog import BaseModel, BasePredictor

ERROR = 'output must not be Optional'


class BadOutput(BaseModel):
    pass


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str) -> Optional[str]:
        return None
