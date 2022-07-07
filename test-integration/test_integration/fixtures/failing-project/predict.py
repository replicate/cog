from cog import BasePredictor
from cog.errors import IrrecoverablePredictorFailure


class Predictor(BasePredictor):
    def predict(self, irrecoverable: bool) -> str:
        if irrecoverable:
            raise IrrecoverablePredictorFailure("irrecoverable error")
        else:
            raise Exception("over budget")
