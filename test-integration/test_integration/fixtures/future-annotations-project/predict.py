from __future__ import annotations

from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, input: str = Input(description="Who to greet")) -> str:
        return "hello " + input
