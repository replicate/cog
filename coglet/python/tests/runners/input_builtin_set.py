from cog import BasePredictor, Input


class Predictor(BasePredictor):
    test_inputs = {'tags': {1, 2, 3}}

    def predict(self, tags: set[int] = Input(default_factory=lambda: {4, 5, 6})) -> str:
        return f'tags: {tags}'
