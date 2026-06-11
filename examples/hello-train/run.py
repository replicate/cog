import requests
from cog import BaseRunner, Input, Path

from typing import Optional


class Runner(BaseRunner):
    def setup(self, weights: Optional[Path] = None):
        if weights:
            self.prefix = requests.get(weights).text
        else:
            self.prefix = "hello"

    def run(
        self, text: str = Input(description="Text to prefix with 'hello ' or weights")
    ) -> str:
        return self.prefix + " " + text
