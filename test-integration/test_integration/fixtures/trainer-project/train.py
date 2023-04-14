from cog import BaseTrainer


class Trainer(BaseTrainer):
    def setup(self) -> None:
        self.hello = "hello "

    def train(self, s: str) -> str:
        return self.hello + s

    def cancel(self) -> None:
        pass
