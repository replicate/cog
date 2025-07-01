from cog import Input


def run(
    prompt: str = Input(),
) -> str:
    return f"HELLO {prompt.upper()}"
