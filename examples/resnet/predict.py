import torch
from PIL import Image
from transformers import AutoImageProcessor, ResNetForImageClassification

from cog import BasePredictor, Input, Path

WEIGHTS_DIR = "/src/weights/resnet50"


class Predictor(BasePredictor):
    def setup(self) -> None:
        self.device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
        self.processor = AutoImageProcessor.from_pretrained(WEIGHTS_DIR)
        self.model = ResNetForImageClassification.from_pretrained(WEIGHTS_DIR)
        self.model = self.model.to(self.device)
        self.model.eval()

    def predict(self, image: Path = Input(description="Image to classify")) -> dict:
        img = Image.open(image).convert("RGB")
        inputs = self.processor(img, return_tensors="pt").to(self.device)

        with torch.no_grad():
            logits = self.model(**inputs).logits

        top3 = logits[0].softmax(0).topk(3)
        labels = self.model.config.id2label
        return {labels[i.item()]: p.item() for p, i in zip(*top3, strict=True)}
