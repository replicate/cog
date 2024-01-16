class Predictor:
    def setup(self) -> None:
        print("did setup")

    async def predict(self, name: str) -> str:
        print(f"hello, {name}")
        return f"hello, {name}"
