from typing import AsyncIterator
from cog import BasePredictor


class Predictor(BasePredictor):
    async def predict(self) -> AsyncIterator[str]:
        yield "foo"
        yield "bar"
        yield "baz"
