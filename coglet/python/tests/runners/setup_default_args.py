from cog import BasePredictor


class Predictor(BasePredictor):
    def setup(self, example_arg: bool = False) -> None:
        print(f'Example Arg: {example_arg}')

    def predict(self) -> str:
        return 'hello'
