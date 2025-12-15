from cog import BasePredictor, Input


class Predictor(BasePredictor):
    test_inputs = {'items': [4, 5, 6]}

    def predict(self, items: list = Input(default=[1, 2, 3])) -> str:
        return f'items: {items}'
