import os
import shutil
from pathlib import Path
from typing import Callable

import pytest
from _pytest.config import Config
from _pytest.main import Session

from .util import random_string, remove_docker_image


def pytest_sessionstart(session: Session) -> None:
    os.environ["COG_NO_UPDATE_CHECK"] = "1"


@pytest.fixture
def cog_binary(pytestconfig: Config) -> Path:
    """Get the path to the cog binary used in integration tests."""
    if os.environ.get("COG_BINARY"):
        cog_path = Path(os.environ["COG_BINARY"])
        if not cog_path.is_absolute():
            # Only make relative to rootdir if it's a relative path
            rootdir = Path(pytestconfig.rootdir)
            cog_path = rootdir / cog_path
        return cog_path.resolve()

    # Check if cog exists in project root.
    # this is where integration tests dump the test build
    project_cog = Path(pytestconfig.rootdir) / "cog"
    if project_cog.exists():
        return project_cog

    # Fall back to cog in PATH
    cog_path = shutil.which("cog")
    if cog_path:
        return Path(cog_path)

    raise FileNotFoundError("Could not find cog binary")


@pytest.fixture
def docker_image_name() -> str:
    return "cog-test-" + random_string(10)


@pytest.fixture
def docker_image(docker_image_name: str) -> str:
    yield docker_image_name
    # We expect the image to exist by this point and will fail if it doesn't.
    # If you just need a name, use docker_image_name.
    remove_docker_image(docker_image_name)


@pytest.fixture
def fixture(tmp_path_factory: pytest.TempPathFactory) -> Callable[[str], Path]:
    """
    Return a function that gives each test an isolated copy of a fixture dir.
    """

    def _make(name: str) -> Path:
        src = Path(__file__).parent / "fixtures" / name
        dst = tmp_path_factory.mktemp(name)  # unique dir per test
        shutil.copytree(src, dst, dirs_exist_ok=True)
        return dst

    return _make
