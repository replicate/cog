from cog import BasePredictor, Input


class Predictor(BasePredictor):
    test_inputs = {'data': {'key': 'value'}}

    def predict(
        self, data: dict[str, str] = Input(default_factory=lambda: {'default': 'value'})
    ) -> str:
        return f'data: {data}'
