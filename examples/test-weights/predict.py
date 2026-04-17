# Throwaway predictor used by examples/test-weights/cog.yaml.
#
# The purpose of this example is to exercise the v1 managed-weights OCI
# pipeline end-to-end: we only care that `cog build` produces a valid
# model image and `cog push` emits an OCI index referencing both the
# image and the packed weight manifests. The predict body is deliberately
# minimal and does NOT actually load the parakeet weights.

from pathlib import Path

from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def setup(self) -> None:
        # Must match cog.yaml `weights.target`.
        self.weights_dir = Path("/src/weights")

    def predict(
        self,
        filename: str = Input(
            description="A weight filename to stat under /src/weights",
            default="config.json",
        ),
    ) -> str:
        path = self.weights_dir / filename
        if not path.exists():
            return f"{path}: missing"
        return f"{path}: {path.stat().st_size} bytes"
