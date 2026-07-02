import os
import tempfile
import warnings

from replicate.client import Client

from cog import ExperimentalFeatureWarning, Input, Path, Secret

warnings.filterwarnings("ignore", category=ExperimentalFeatureWarning)


def run(
    image: Path = Input(description="Input image to test"),
    replicate_api_token: Secret = Input(
        description="Replicate API token used to call other models",
    ),
) -> Path:
    replicate = Client(api_token=replicate_api_token.get_secret_value())
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
