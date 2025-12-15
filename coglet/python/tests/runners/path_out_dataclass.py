import tempfile
from dataclasses import dataclass

from cog import BasePredictor, Path

try:
    # Compat: coders not available in legacy Cog
    from cog.coder import dataclass_coder  # noqa: F401
except Exception:
    pass


@dataclass(frozen=True)
class Output:
    p: Path


class Predictor(BasePredictor):
    test_inputs = {'s': 'hello'}

    def predict(self, s: str) -> Output:
        with tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False) as f:
            f.write(f'*{s}*')
        return Output(p=Path(f.name))
