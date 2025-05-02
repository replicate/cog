import os
import pathlib
import shutil
import subprocess
from pathlib import Path

DEFAULT_TIMEOUT = 60


def test_migrate(tmpdir_factory):
    project_dir = Path(__file__).parent / "fixtures/migration-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        [
            "cog",
            "migrate",
            "--y",
        ],
        cwd=out_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=DEFAULT_TIMEOUT,
    )
    assert result.returncode == 0
    with open(os.path.join(out_dir, "cog.yaml"), encoding="utf8") as handle:
        assert handle.read(), """build:
  python_version: "3.11"
  python_requirements: requirements.txt
  fast: true
predict: predict.py:Predictor
"""
    with open(os.path.join(out_dir, "predict.py"), encoding="utf8") as handle:
        assert handle.read(), """from typing import Optional
from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, s: Optional[str] = Input(description="My Input Description", default=None)) -> str:
        return "hello " + s
"""
    with open(os.path.join(out_dir, "requirements.txt"), encoding="utf8") as handle:
        assert handle.read(), "pillow"
