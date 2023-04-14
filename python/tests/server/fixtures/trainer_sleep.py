import time


class Trainer:
    def setup(self) -> None:
        print("Setting up.")

    def train(self, sleep: float = 0) -> str:
        time.sleep(sleep)
        return f"done in {sleep} seconds"

    def cancel(self) -> None:
        print("Canceling.")
