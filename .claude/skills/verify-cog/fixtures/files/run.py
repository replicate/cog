from pathlib import Path as FsPath

from cog import BaseRunner, Input, Path


class Runner(BaseRunner):
    def run(self, doc: Path = Input(description="Text file to upper-case")) -> Path:
        out = FsPath("out.txt")
        out.write_text(FsPath(str(doc)).read_text().upper())
        return Path(out)
