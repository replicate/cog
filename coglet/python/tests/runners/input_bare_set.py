from cog import BasePredictor, Input


class Predictor(BasePredictor):
    test_inputs = {'tags': {1, 2, 3}}

    def predict(self, tags: set = Input(default_factory=set)) -> str:
        return f'tags: {tags}'
