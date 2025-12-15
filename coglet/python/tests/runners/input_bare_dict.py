from cog import BasePredictor, Input


class Predictor(BasePredictor):
    test_inputs = {'data': {'key': 'value'}}

    def predict(self, data: dict = Input(default_factory=dict)) -> str:
        return f'data: {data}'
