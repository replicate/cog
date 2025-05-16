from cog import BasePredictor


class Trainer(BasePredictor):
    def setup(self) -> None:
        print("Trainer is setting up.")

    def predict(self, s: str) -> str:
        return "hello predict " + s
