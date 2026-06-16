from typing import List, Optional

from cog import BaseRunner, File, Input, Path


class Runner(BaseRunner):
    def run(
        self,
        text: str = Input(description="Required anchor field"),
        # PEP 604 optional lists
        opt_tags: list[str] | None = Input(
            description="Optional list of strings", default=None
        ),
        opt_paths: list[Path] | None = Input(
            description="Optional list of paths", default=None
        ),
        opt_files: list[File] | None = Input(
            description="Optional list of files", default=None
        ),
        # typing.Optional style
        opt_ints: Optional[List[int]] = Input(
            description="Optional list of ints", default=None
        ),
    ) -> str:
        return f"{text}"
