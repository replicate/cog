from typing import List
from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(
        self, fruits: List[str] = Input(description="array of fruit strings")
    ) -> str:
        return "fruits: " + ", ".join(fruits)
