"""Nox sessions for cog Python SDK testing."""

import glob
import os

import nox

# Use uv for venv creation and Python management (uv auto-downloads Python if needed)
nox.options.default_venv_backend = "uv"

PYTHON_VERSIONS = ["3.10", "3.11", "3.12", "3.13"]
PYTHON_DEFAULT = "3.13"

# Test dependencies (mirrored from pyproject.toml [dependency-groups].test)
TEST_DEPS = [
    "httpx",
    "hypothesis",
    "numpy",
    "pillow",
    "pytest",
    "pytest-asyncio",
    "pytest-httpserver",
    "pytest-timeout",
    "pytest-xdist",
    "pytest-cov",
    "responses",
    "pexpect",
]


def _install_package(session: nox.Session) -> None:
    """Install the package, using pre-built wheel if available."""
    wheels = glob.glob("dist/cog-*.whl")
    if wheels and os.environ.get("CI"):
        # In CI, use pre-built wheel
        session.install(wheels[0])
    else:
        # Local dev: editable install
        session.install("-e", ".")


@nox.session(python=PYTHON_VERSIONS if not os.environ.get("CI") else False)
def tests(session: nox.Session) -> None:
    """Run the test suite."""
    _install_package(session)
    session.install(*TEST_DEPS)
    args = session.posargs or ["-n", "auto", "-vv"]
    session.run(
        "pytest",
        "python/tests",
        "--cov=python/cog",
        "--cov-report=term-missing:skip-covered",
        *args,
    )


@nox.session(python=PYTHON_DEFAULT if not os.environ.get("CI") else False)
def typecheck(session: nox.Session) -> None:
    """Run type checking with pyright."""
    _install_package(session)
    session.install("pyright==1.1.375")
    session.run("pyright", *session.posargs)
