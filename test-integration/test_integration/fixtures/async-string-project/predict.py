from cog import BasePredictor


class Predictor(BasePredictor):
    async def predict(self, s: str) -> str:
        return "hello " + s
