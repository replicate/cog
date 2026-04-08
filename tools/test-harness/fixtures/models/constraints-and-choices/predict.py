from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(
        self,
        prompt: str = Input(description="The prompt", default="hello"),
        temperature: float = Input(
            description="Sampling temperature", ge=0.0, le=2.0, default=0.7
        ),
        top_k: int = Input(description="Top-K", ge=1, le=100, default=50),
        mode: str = Input(
            description="Quality mode",
            choices=["fast", "balanced", "quality"],
            default="balanced",
        ),
        style: int = Input(
            description="Style preset",
            choices=[1, 2, 3],
            default=1,
        ),
    ) -> str:
        return f"{prompt}-{temperature}-{top_k}-{mode}-{style}"
