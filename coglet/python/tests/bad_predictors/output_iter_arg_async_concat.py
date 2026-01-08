from cog import AsyncConcatenateIterator, BaseModel, BasePredictor

ERROR = 'iterator type must have a type argument'


class BadOutput(BaseModel):
    pass


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    async def predict(self, s: str) -> AsyncConcatenateIterator:
        yield None
