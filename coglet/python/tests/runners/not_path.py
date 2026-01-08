from cog import BasePredictor


class Predictor(BasePredictor):
    test_inputs = {'s': 'https://replicate.com'}

    def predict(self, s: str) -> str:
        return f'*{s}*'
