from cog import BasePredictor, Input, Secret


class Predictor(BasePredictor):
    def predict(
        self,
        text: str = Input(description="A string input"),
        count: int = Input(description="An integer", default=5),
        temperature: float = Input(description="A float", default=0.7),
        flag: bool = Input(description="A boolean", default=True),
        api_key: Secret = Input(description="A secret key"),
    ) -> str:
        return f"{text}-{count}-{temperature}-{flag}"
