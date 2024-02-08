from cog import BasePredictor, Path

from typing import List, Union


class Predictor(BasePredictor):
    def predict(self, args: Union[int, List[int]]) -> int:
        if isinstance(args, int):
            return args
        else:
            return sum(args)
