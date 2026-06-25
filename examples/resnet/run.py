import os

os.environ["TORCH_HOME"] = "."

import torch
from PIL import Image
from torchvision import models

from cog import BaseRunner, Input, Path

WEIGHTS = models.ResNet50_Weights.IMAGENET1K_V1


class Runner(BaseRunner):
    def setup(self) -> None:
        self.device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
        self.model = models.resnet50(weights=WEIGHTS).to(self.device)
        self.model.eval()

    def run(self, image: Path = Input(description="Image to classify")) -> dict:
        img = Image.open(image).convert("RGB")
        inputs = WEIGHTS.transforms()(img).unsqueeze(0).to(self.device)

        with torch.no_grad():
            preds = self.model(inputs)

        top3 = preds[0].softmax(0).topk(3)
        categories = WEIGHTS.meta["categories"]
        return {categories[i]: p.item() for p, i in zip(*top3, strict=True)}
