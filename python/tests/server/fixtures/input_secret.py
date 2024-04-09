from cog import BasePredictor, Secret


class Predictor(BasePredictor):
    def predict(self, secret: Secret) -> str:
        return secret.get_secret_value()
