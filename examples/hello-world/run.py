from cog import BaseRunner, Input


class Runner(BaseRunner):
    def setup(self):
        self.prefix = "hello"

    def run(self, text: str = Input(description="Text to prefix with 'hello '")) -> str:
        return self.prefix + " " + text
