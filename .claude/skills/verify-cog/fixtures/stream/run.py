import time
from typing import Iterator

from cog import BaseRunner, Input, streaming


class Runner(BaseRunner):
    @streaming
    def run(
        self,
        words: str = Input(description="Space separated words to stream back"),
        delay: float = Input(
            description="Seconds to sleep between words", default=0.0, ge=0, le=30
        ),
    ) -> Iterator[str]:
        for word in words.split():
            time.sleep(delay)
            yield word
