import pathlib
import shutil
import subprocess
from pathlib import Path


def test_train_takes_input_and_produces_weights(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/train-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        [cog_binary, "train", "--debug", "-i", "n=42"],
        cwd=out_dir,
        check=False,
        capture_output=True,
    )
    assert result.returncode == 0
    assert result.stdout == b""
    with open(out_dir / "weights.bin", "rb") as f:
        assert len(f.read()) == 42
    assert "falling back to slow loader" not in str(result.stderr)


def test_train_pydantic2(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/pydantic2-output"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        [cog_binary, "train", "--debug", "-i", 'some_input="hello"'],
        cwd=out_dir,
        check=False,
        capture_output=True,
    )
    assert result.returncode == 0


def test_training_setup_project(tmpdir_factory, cog_binary):
    project_dir = Path(__file__).parent / "fixtures/training-setup-project"
    out_dir = pathlib.Path(tmpdir_factory.mktemp("project"))
    shutil.copytree(project_dir, out_dir, dirs_exist_ok=True)
    result = subprocess.run(
        [cog_binary, "train", "--debug", "-i", "s=world"],
        cwd=out_dir,
        check=False,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0
    assert "Trainer is setting up." in str(result.stderr)
    with open(out_dir / "weights", "r", encoding="utf8") as f:
        assert f.read() == "hello train world"
