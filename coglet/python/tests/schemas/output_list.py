from cog import BaseModel, BasePredictor


class Output(BaseModel):
    x: int
    y: str


FIXTURE = [
    ({}, [Output(x=1, y='a'), Output(x=2, y='b'), Output(x=3, y='c')]),
]


class Predictor(BasePredictor):
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(self) -> list[Output]:
        return [Output(x=1, y='a'), Output(x=2, y='b'), Output(x=3, y='c')]
