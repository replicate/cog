import sys


def _get_version() -> str:
    try:
        if sys.version_info >= (3, 8):
            from importlib.metadata import (
                version,  # pylint: disable=import-outside-toplevel
            )

            return version("cog")
        else:
            import pkg_resources  # pylint: disable=import-outside-toplevel

            return pkg_resources.get_distribution("cog").version
    except Exception:  # pylint: disable=broad-exception-caught
        return "unknown"


def get_user_agent() -> str:
    return f"cog-worker/{_get_version()}"
