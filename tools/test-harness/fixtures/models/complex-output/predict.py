from cog import BaseModel, BasePredictor, Input


class Output(BaseModel):
    text: str
    score: float
    tags: list[str]


class Predictor(BasePredictor):
    def predict(
        self,
        prompt: str = Input(description="Input prompt"),
    ) -> Output:
        return Output(text=f"generated: {prompt}", score=0.95, tags=["a", "b"])
