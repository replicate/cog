from cog import BaseRunner, ConcatenateIterator, Input


class Runner(BaseRunner):
    def run(
        self, text: str = Input(description="Text to prefix with 'hello there, '")
    ) -> ConcatenateIterator[str]:
        yield "hello "
        yield "there, "
        yield text
