import tempfile

from cog import BaseModel, BasePredictor, Path


class Output(BaseModel):
    p: Path


class Predictor(BasePredictor):
    test_inputs = {'s': 'hello'}

    def predict(self, s: str) -> Output:
        with tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False) as f:
            f.write(f'*{s}*')
        return Output(p=Path(f.name))
