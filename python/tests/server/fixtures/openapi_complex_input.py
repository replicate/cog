from cog import BasePredictor, File, Input, Path


class Predictor(BasePredictor):
    def predict(
        self,
        no_default: str,
        default_without_input: str = "default",
        input_with_default: int = Input(default=-10),
        path: Path = Input(description="Some path"),
        image: File = Input(description="Some path"),
        choices: str = Input(choices=["foo", "bar"]),
        int_choices: int = Input(choices=[3, 4, 5]),
    ) -> str:
        pass
