import os

os.environ["HF_HUB_CACHE"] = "./.cache"
os.environ["HF_XET_HIGH_PERFORMANCE"] = "1"


import tempfile

import torch
from diffusers import ZImagePipeline

from cog import BaseRunner, Path


class Runner(BaseRunner):
    def setup(self) -> None:
        self.model = ZImagePipeline.from_pretrained(
            "Tongyi-MAI/Z-Image-Turbo",
            torch_dtype=torch.bfloat16,
            low_cpu_mem_usage=False,
        )
        self.model.to("cuda")

    def run(self, prompt: str) -> Path:
        image = self.model(
            prompt=prompt,
            height=1024,
            width=1024,
            num_inference_steps=9,  # This actually results in 8 DiT forwards
            guidance_scale=0.0,  # Guidance should be 0 for the Turbo models
            generator=torch.Generator("cuda").manual_seed(42),
        ).images[0]
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as f:
            output_path = Path(f.name)
        image.save(output_path)
        return output_path
