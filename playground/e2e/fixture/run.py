import time

from cog import BaseRunner, ConcatenateIterator, Input, streaming


class Runner(BaseRunner):
    @streaming
    def run(
        self,
        text: str = Input(
            description="Text to prefix with 'hello '",
            min_length=2,
            max_length=20,
            regex=r"^[a-z ]+$",
        ),
    ) -> ConcatenateIterator[str]:
        yield "hello "
        if text == "slow":
            time.sleep(2)
        yield text
