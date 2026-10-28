from cog import BaseRunner, Input


class Runner(BaseRunner):
    def setup(self) -> None:
        self.prefix = "verify"

    def run(
        self,
        text: str = Input(description="Text to echo"),
        times: int = Input(description="Repeat count", default=2, ge=1, le=5),
    ) -> str:
        return " ".join(f"{self.prefix}:{text}" for _ in range(times))
