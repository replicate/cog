import asyncio
import os

from cog import BasePredictor, current_scope


class Predictor(BasePredictor):
    test_inputs = {'i': 0, 's': 'foo'}

    async def setup(self) -> None:
        print('starting async setup')
        i = int(os.environ.get('SETUP_SLEEP', '0'))
        for x in range(i):
            print(f'setup in progress {x + 1}/{i}')
            await asyncio.sleep(0.5)
        print('completed async setup')

    async def predict(self, i: int, s: str) -> str:
        try:
            await asyncio.sleep(0.1)
            print('starting async prediction')
            if i > 0:
                await asyncio.sleep(0.6)
            for x in range(i):
                print(f'prediction in progress {x + 1}/{i}')
                await asyncio.sleep(0.6)
            print('completed async prediction')
            await asyncio.sleep(0.1)
            current_scope().record_metric('i', i)
            current_scope().record_metric('s_len', len(s))
            return f'*{s}*'
        except asyncio.CancelledError as e:
            print('prediction canceled')
            raise e
