from cog import File
import io

def train(text: str) -> File:
    return io.StringIO(text)
