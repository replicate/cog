import time

from cog import BaseRunner, ConcatenateIterator, Input, streaming


class Runner(BaseRunner):
    @streaming
    def run(
        self, text: str = Input(description="Text to prefix with 'hello '")
    ) -> ConcatenateIterator[str]:
        yield "hello "
        if text == "slow":
            time.sleep(2)
        yield text
