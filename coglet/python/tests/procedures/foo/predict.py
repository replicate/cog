import asyncio

from cog import current_scope


async def predict(i: int, s: str) -> str:
    print('predicting foo')
    await asyncio.sleep(i)
    token = current_scope().context['replicate_api_token']
    return f'i={i}, s={s}, token={token}'
