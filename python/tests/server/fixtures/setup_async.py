class Predictor:
    async def download(self) -> None:
        print("download complete!")

    async def setup(self) -> None:
        print("setup starting...")
        await self.download()
        print("setup complete!")

    async def predict(self) -> str:
        print("running prediction")
        return "output"
