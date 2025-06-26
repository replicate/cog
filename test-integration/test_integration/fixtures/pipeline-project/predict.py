from cog import Input
from replicate import use

upcase = use("pipelines-beta/upcase")


def run(
    prompt: str = Input(),
) -> str:
    upcased_prompt = upcase(prompt=prompt)
    return f"HELLO {upcased_prompt}"
