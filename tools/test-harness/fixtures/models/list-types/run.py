from typing import List

from cog import BaseRunner, File, Input, Path


class Runner(BaseRunner):
    def run(
        self,
        tags: list[str] = Input(description="List of strings"),
        numbers: List[int] = Input(description="List of ints"),
        paths: list[Path] = Input(description="List of paths"),
        files: list[File] = Input(description="List of files"),
    ) -> str:
        return f"tags={len(tags)} numbers={len(numbers)}"
