import os
from pathlib import Path
import subprocess


def test_trainer():
    project_dir = Path(__file__).parent / "fixtures/trainer-project"
    subprocess.run(
        ["cog", "train", "-i", "world"],
        cwd=project_dir,
        check=True,
    )
    weights_file = os.path.join(project_dir, "weights")
    with open(weights_file, "r") as f:
        assert f.read() == "hello world"


def test_train():
    project_dir = Path(__file__).parent / "fixtures/train-project"
    subprocess.run(
        ["cog", "train", "-i", "world"],
        cwd=project_dir,
        check=True,
    )
    weights_file = os.path.join(project_dir, "weights")
    with open(weights_file, "r") as f:
        assert f.read() == "hello world"
