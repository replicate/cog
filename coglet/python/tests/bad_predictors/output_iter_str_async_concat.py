from cog import AsyncConcatenateIterator, BaseModel, BasePredictor

ERROR = 'AsyncConcatenateIterator must have str element'


class BadOutput(BaseModel):
    pass


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    async def predict(self, s: str) -> AsyncConcatenateIterator[int]:
        yield 0
