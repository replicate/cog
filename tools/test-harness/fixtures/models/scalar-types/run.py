from cog import BaseRunner, Input


class Runner(BaseRunner):
    def run(
        self,
        text: str = Input(description="A string input"),
        count: int = Input(description="An integer", default=5),
        temperature: float = Input(description="A float", default=0.7),
        flag: bool = Input(description="A boolean", default=True),
    ) -> str:
        return f"{text}-{count}-{temperature}-{flag}"
