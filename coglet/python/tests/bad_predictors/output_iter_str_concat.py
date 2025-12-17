from cog import BaseModel, BasePredictor, ConcatenateIterator

ERROR = 'ConcatenateIterator must have str element'


class BadOutput(BaseModel):
    pass


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    async def predict(self, s: str) -> ConcatenateIterator[int]:
        yield 0
