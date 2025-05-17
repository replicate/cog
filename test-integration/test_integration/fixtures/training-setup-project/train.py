from cog import BasePredictor


class Trainer(BasePredictor):
    def setup(self) -> None:
        print("Trainer is setting up.")

    def train(self, s: str) -> str:
        print("Trainer.train called.")
        return "hello train " + s

    def predict(self, s: str) -> str:
        print("Trainer.predict called.")
        return "hello predict " + s
