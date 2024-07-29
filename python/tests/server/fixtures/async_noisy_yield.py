import asyncio
from typing import AsyncIterator, Optional
from cog import BasePredictor, Path, Input


class Predictor(BasePredictor):
    async def predict(
        self,
        n: int = 100,
        sleep: float = 0.0001,
    ) -> AsyncIterator[str]:
        for i in range(n):
            #print("hi")
            yield f"hello {i}"
            await asyncio.sleep(sleep)
