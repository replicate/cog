from cog import BaseTrainer


class Trainer(BaseTrainer):
    def setup(self) -> None:
        self.num = 42

    def train(self) -> int:
        return self.num
