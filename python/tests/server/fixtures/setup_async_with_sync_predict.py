class Predictor:
    async def download(self) -> None:
        print("setup used asyncio.run! it's not very effective...")

    async def setup(self) -> None:
        await self.download()

    def predict(self) -> str:
        return "output"
