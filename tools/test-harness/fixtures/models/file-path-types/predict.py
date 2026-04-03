from cog import BasePredictor, File, Input, Path


class Predictor(BasePredictor):
    def predict(
        self,
        image: Path = Input(description="An image path"),
        document: File = Input(description="A file upload"),
        # Optional variants
        mask: Path | None = Input(description="Optional mask path", default=None),
        attachment: File | None = Input(description="Optional file", default=None),
    ) -> str:
        return f"image={image} mask={mask}"
