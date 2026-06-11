import os

os.environ["HF_HUB_CACHE"] = "./.cache"
os.environ["HF_XET_HIGH_PERFORMANCE"] = "1"


import tempfile

import torch
from cog import BaseRunner, Path
from diffusers import ZImagePipeline


class Runner(BaseRunner):
    def setup(self):
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
        output_path = Path(tempfile.mktemp(suffix=".png"))
        image.save(output_path)
        return output_path
