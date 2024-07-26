def _get_version() -> str:
    try:
        try:
            from importlib.metadata import (  # pylint: disable=import-outside-toplevel
                version,
            )
        except ImportError:
            pass
        else:
            return version("cog")
        import pkg_resources  # pylint: disable=import-outside-toplevel

        return pkg_resources.get_distribution("cog").version
    except Exception:  # pylint: disable=broad-exception-caught
        return "unknown"


def get_user_agent() -> str:
    return f"cog-worker/{_get_version()}"
