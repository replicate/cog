from cog import BaseRunner, Path

class Runner(BaseRunner):
    def run(self) -> Path:
        return Path("hello.webp")
