import tempfile

from pydantic import BaseModel

from cog import BasePredictor, Path

try:
    # Compat: coders not available in legacy Cog
    from cog.coder import pydantic_coder  # noqa: F401
except Exception:
    pass


class Output(BaseModel):
    p: Path


class Predictor(BasePredictor):
    test_inputs = {'s': 'hello'}

    def predict(self, s: str) -> Output:
        with tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False) as f:
            f.write(f'*{s}*')
        return Output(p=Path(f.name))
