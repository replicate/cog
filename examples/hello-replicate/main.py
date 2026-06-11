import tempfile
import os
import warnings
from cog import Input, Path, ExperimentalFeatureWarning

from replicate.client import Client

warnings.filterwarnings("ignore", category=ExperimentalFeatureWarning)


def run(
    image: Path = Input(description="Input image to test"),
) -> Path:
    replicate = Client()
    claude_prompt = """
You have been asked to generate a prompt for an image model that should re-create the
image provided to you exactly. Please describe the provided image in great detail
paying close attention to the contents, layout and style.
    """
    prompt = replicate.run(
        "anthropic/claude-4-sonnet", input={"prompt": claude_prompt, "image": image}
    )
    output = replicate.run(
        "black-forest-labs/flux-dev", input={"prompt": "".join(prompt)}
    )

    with tempfile.TemporaryDirectory(delete=False) as tmpdir:
        dest_path = os.path.join(tmpdir, "output.webp")
        with open(dest_path, "wb") as file:
            file.write(output[0].read())
        return Path(dest_path)
