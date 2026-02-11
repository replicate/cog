"""Nox sessions for cog Python SDK testing."""

import glob

import nox

# Use uv for venv creation and Python management (uv auto-downloads Python if needed)
nox.options.default_venv_backend = "uv"

PYTHON_VERSIONS = ["3.10", "3.11", "3.12", "3.13"]
PYTHON_DEFAULT = "3.13"

# Test dependencies (mirrored from pyproject.toml [dependency-groups].test)
TEST_DEPS = [
    "pytest",
    "pytest-timeout",
    "pytest-xdist",
    "pytest-cov",
]


def _install_coglet(session: nox.Session) -> None:
    """Install coglet wheel (required dependency).

    Falls back to PyPI with --prerelease=allow since coglet
    may only have pre-release versions available.
    """
    coglet_wheels = glob.glob("dist/coglet-*.whl")
    if coglet_wheels:
        session.install(coglet_wheels[0])
    else:
        session.install("--prerelease=allow", "coglet")


def _install_package(session: nox.Session) -> None:
    """Install the package, using pre-built wheel if available."""
    _install_coglet(session)
    wheels = glob.glob("dist/cog-*.whl")
    if wheels:
        # Use pre-built wheel if available
        session.install(wheels[0])
    else:
        # Editable install
        session.install("-e", ".")


@nox.session(python=PYTHON_VERSIONS)
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


@nox.session(python=PYTHON_DEFAULT)
def typecheck(session: nox.Session) -> None:
    """Run type checking with pyright."""
    _install_package(session)
    session.install("pyright==1.1.375")
    session.run("pyright", *session.posargs)


@nox.session(name="coglet", python=PYTHON_VERSIONS)
def coglet_tests(session: nox.Session) -> None:
    """Run coglet-python binding tests."""
    _install_package(session)
    session.install("pytest", "requests")
    session.run("pytest", "crates/coglet-python/tests", "-v", *session.posargs)
