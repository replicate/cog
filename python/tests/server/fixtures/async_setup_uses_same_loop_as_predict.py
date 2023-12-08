import asyncio


class Predictor:
    async def setup(self) -> None:
        self.loop = asyncio.get_running_loop()

    async def predict(self) -> bool:
        return self.loop == asyncio.get_running_loop()
