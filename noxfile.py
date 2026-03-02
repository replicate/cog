"""Nox sessions for cog Python SDK testing."""

import glob
import platform

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


def _find_compatible_wheel(pattern: str) -> str | None:
    """Find a wheel matching the current platform from dist/.

    Returns None when no wheels exist at all.  Raises RuntimeError when
    wheels exist but none are compatible — that means the build produced
    the wrong platform and should be fixed, not silently papered over.
    """
    wheels = glob.glob(pattern)
    if not wheels:
        return None

    system = platform.system().lower()
    machine = platform.machine().lower()
    platform_tags = {
        ("darwin", "arm64"): "macosx",
        ("darwin", "x86_64"): "macosx",
        ("linux", "x86_64"): "manylinux",
        ("linux", "aarch64"): "manylinux",
    }
    tag = platform_tags.get((system, machine))
    if tag:
        for whl in wheels:
            if tag in whl or "none-any" in whl:
                return whl
        raise RuntimeError(
            f"Found wheel(s) in dist/ but none compatible with {system}/{machine}:\n"
            + "\n".join(f"  {w}" for w in wheels)
            + "\nRun 'mise run build:coglet:wheel' to build a native wheel."
        )

    # Unknown platform — let pip figure it out
    return wheels[0]


def _install_coglet(session: nox.Session) -> None:
    """Install coglet wheel (required dependency)."""
    whl = _find_compatible_wheel("dist/coglet-*.whl")
    if whl:
        session.install(whl)
    else:
        session.error(
            "No coglet wheel found in dist/. Run 'mise run build:coglet:wheel' first."
        )


def _install_package(session: nox.Session) -> None:
    """Install the cog SDK and coglet dependency."""
    _install_coglet(session)
    # Always use editable install for the SDK so tests run against
    # the working tree, not a stale wheel from a previous build.
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
