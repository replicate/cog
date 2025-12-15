from cog import BasePredictor, Input


class Predictor(BasePredictor):
    test_inputs = {'items': [10, 20]}

    def predict(self, items: list = Input(default=[1, 2, 3])) -> str:
        # Mutate the list to test isolation
        items.append(999)
        return f'items: {items}'
