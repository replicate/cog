import asyncio


class Predictor:
    async def download(self) -> None:
        print("setup used asyncio.run! it's not very effective...")

    def setup(self) -> None:
        asyncio.run(self.download())

    def predict(self) -> str:
        return "output"
