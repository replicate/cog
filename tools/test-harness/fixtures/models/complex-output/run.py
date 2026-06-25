from cog import BaseModel, BaseRunner, Input


class Output(BaseModel):
    text: str
    score: float
    tags: list[str]


class Runner(BaseRunner):
    def run(
        self,
        prompt: str = Input(description="Input prompt"),
    ) -> Output:
        return Output(text=f"generated: {prompt}", score=0.95, tags=["a", "b"])
