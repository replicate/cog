import asyncio
from typing import AsyncIterator

from cog import BasePredictor


class Predictor(BasePredictor):
    test_inputs = {'i': 3, 's': 'foo'}

    async def predict(self, i: int, s: str) -> AsyncIterator[str]:
        await asyncio.sleep(0.1)
        print('starting prediction')
        if i > 0:
            await asyncio.sleep(0.6)
        for x in range(i):
            print(f'prediction in progress {x + 1}/{i}')
            await asyncio.sleep(0.6)
            yield f'*{s}-{x}*'
            await asyncio.sleep(0.6)
        print('completed prediction')
