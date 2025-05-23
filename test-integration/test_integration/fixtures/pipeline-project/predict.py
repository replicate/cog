from cog import Input
from cog.ext.pipelines import include

upcase = include("pipelines-beta/upcase")


def run(
    prompt: str = Input(),
) -> str:
    upcased_prompt = upcase(prompt=prompt)
    return f"HELLO {upcased_prompt}"
