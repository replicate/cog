from cog import BasePredictor, Input


class Predictor(BasePredictor):
    test_inputs = {'message': 'test message'}

    def predict(self, message: str = Input(default='hello world')) -> str:
        return f'message: {message}'
