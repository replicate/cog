from __future__ import annotations

from cog import BaseModel, BasePredictor

ERROR = 'predictor with "from __future__ import annotations" is not supported'


class BadOutput(BaseModel):
    pass


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str) -> str:
        return s
