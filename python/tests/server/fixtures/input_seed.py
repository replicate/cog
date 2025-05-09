from cog import BasePredictor, Seed


class Predictor(BasePredictor):
    def predict(self, seed: Seed) -> int:
        return seed
