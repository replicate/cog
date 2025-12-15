import time

from cog import current_scope


def predict(i: int, s: str) -> str:
    print('predicting bar')
    time.sleep(i)
    token = current_scope().context['replicate_api_token']
    return f'i={i}, s={s}, token={token}'
