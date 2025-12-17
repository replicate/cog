import tempfile
from typing import Any

from cog import BasePredictor, Path

try:
    # Compat: coders not available in legacy Cog
    from cog.coder import json_coder  # noqa: F401
except Exception:
    pass


class Predictor(BasePredictor):
    test_inputs = {'s': 'hello'}

    def predict(self, s: str) -> dict[str, Any]:
        with tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False) as f:
            f.write(f'*{s}*')
        return {'p': Path(f.name)}
