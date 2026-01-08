import asyncio

from cog import AsyncConcatenateIterator

test_inputs = {'i': 3, 's': 'foo'}


async def predict(i: int, s: str) -> AsyncConcatenateIterator[str]:
    await asyncio.sleep(0.1)
    print('starting prediction')
    if i > 0:
        await asyncio.sleep(0.1)
    for x in range(i):
        print(f'prediction in progress {x + 1}/{i}')
        await asyncio.sleep(0.1)
        yield f'*{s}-{x}*'
        await asyncio.sleep(0.1)
    print('completed prediction')
