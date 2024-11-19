import asyncio

from cog import BasePredictor


class Predictor(BasePredictor):
    async def predict(self, s: str, sleep: float) -> str:
        await asyncio.sleep(sleep)
        return f"wake up {s}"
