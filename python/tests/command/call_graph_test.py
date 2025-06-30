import os
import tempfile
from pathlib import Path

import pytest

from cog.command.call_graph import analyze_python_file


def test_call_graph():
    with tempfile.TemporaryDirectory() as tmpdir:
        filepath = os.path.join(tmpdir, "predict.py")
        with open(filepath, "w", encoding="utf8") as handle:
            handle.write("""from cog import Path, Input
from replicate import use

flux_schnell = use("black-forest-labs/flux-schnell")

def run(
    prompt: str = Input(description="Describe the image to generate"),
    seed: int = Input(description="A seed", default=0)
) -> Path:
    output_url = flux_schnell(prompt=prompt, seed=seed)[0]
    return output_url
""")
        includes = analyze_python_file(Path(filepath))
        assert includes == ["black-forest-labs/flux-schnell"]


def test_call_graph_with_dynamic_string():
    with tempfile.TemporaryDirectory() as tmpdir:
        filepath = os.path.join(tmpdir, "predict.py")
        with open(filepath, "w", encoding="utf8") as handle:
            handle.write("""from cog import Path, Input
from replicate import use

i = 2
flux_schnell = use(f"black-forest-labs/flux-schnell-{i}")

def run(
    prompt: str = Input(description="Describe the image to generate"),
    seed: int = Input(description="A seed", default=0)
) -> Path:
    output_url = flux_schnell(prompt=prompt, seed=seed)[0]
    return output_url
""")
        with pytest.raises(ValueError) as excinfo:
            analyze_python_file(Path(filepath))
        assert str(excinfo.value).endswith(
            "Unresolvable argument at line 5: Not a string literal"
        )


def test_call_graph_include_constructed_in_local_scope():
    with tempfile.TemporaryDirectory() as tmpdir:
        filepath = os.path.join(tmpdir, "predict.py")
        with open(filepath, "w", encoding="utf8") as handle:
            handle.write("""from cog import Path, Input
from replicate import use

def run(
    prompt: str = Input(description="Describe the image to generate"),
    seed: int = Input(description="A seed", default=0)
) -> Path:
    flux_schnell = use("black-forest-labs/flux-schnell")
    output_url = flux_schnell(prompt=prompt, seed=seed)[0]
    return output_url
""")
        with pytest.raises(ValueError) as excinfo:
            analyze_python_file(Path(filepath))
        assert str(excinfo.value).endswith(
            "Invalid scope at line 8: `replicate.use(...)` must be in global scope"
        )
