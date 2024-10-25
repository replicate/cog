import asyncio

from cog import BasePredictor


class Predictor(BasePredictor):
    async def predict(self, sleep: float = 0) -> str:
        print("starting")
        await asyncio.sleep(sleep)
        return f"done in {sleep} seconds"
