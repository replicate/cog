from cog import ConcatenateIterator

test_inputs = {'i': 3, 's': 'foo'}


async def predict(i: int, s: str) -> ConcatenateIterator[str]:
    print('starting prediction')
    if i > 0:
        for x in range(i):
            print(f'prediction in progress {x + 1}/{i}')
            yield f'*{s}-{x}*'
    print('completed prediction')
