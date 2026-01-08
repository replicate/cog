from cog import BaseModel, BasePredictor


class CustomOut(BaseModel):
    x: int
    y: str


FIXTURE = [
    ({'i': 3}, [CustomOut(x=3, y='a'), CustomOut(x=2, y='a'), CustomOut(x=1, y='a')]),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self, i: int) -> list[CustomOut]:
        outputs: list[CustomOut] = []
        while i > 0:
            outputs.append(CustomOut(x=i, y='a'))
            i -= 1
        return outputs
