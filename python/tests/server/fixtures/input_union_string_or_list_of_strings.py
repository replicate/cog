from cog import BasePredictor, Path

from typing import List, Union


class Predictor(BasePredictor):
    def predict(self, args: Union[str, List[str]]) -> str:
        if isinstance(args, str):
            return args
        else:
            return "".join(args)
