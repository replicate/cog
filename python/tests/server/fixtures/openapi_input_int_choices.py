from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, pick_a_number_any_number: int = Input(choices=[1, 2])) -> str:
        pass
