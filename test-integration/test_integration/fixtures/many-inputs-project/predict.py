from cog import BasePredictor, Input, Path


class Predictor(BasePredictor):
    def predict(
        self,
        no_default: str,
        default_without_input: str = "default",
        input_with_default: int = Input(default=10),
        path: Path = Input(description="Some path"),
        image: Path = Input(description="Some path"),
        choices: str = Input(choices=["foo", "bar"]),
        int_choices: int = Input(description="hello", choices=[3, 4, 5]),
    ) -> str:
        with path.open() as f:
            path_contents = f.read()
        image_extension = str(image).split(".")[-1]
        return (
            no_default
            + " "
            + default_without_input
            + " "
            + str(input_with_default * 2)
            + " "
            + path_contents
            + " "
            + image_extension
            + " "
            + choices
            + " "
            + str(int_choices * 2)
        )
