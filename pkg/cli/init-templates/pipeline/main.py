# Prediction interface for Cog ⚙️
# https://cog.run/python

from cog import Path, Input
import replicate

flux_schnell = replicate.use("black-forest-labs/flux-schnell")
claude = replicate.use("anthropic/claude-3.5-haiku")


def run(
    prompt: str = Input(description="Describe the image to generate"),
    seed: int = Input(description="A seed", default=0),
) -> Path:
    detailed_prompt = claude(
        prompt=f"""
    Generate a detailed prompt for a generative image model that will
    generate a high quality dynamic image based on the following
    theme: {prompt}
    """
    )
    output_paths = flux_schnell(prompt=detailed_prompt, seed=seed)

    return Path(output_paths[0])
