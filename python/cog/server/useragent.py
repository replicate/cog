def _get_version() -> str:
    try:
        try:
            from importlib.metadata import version
        except ImportError:
            pass
        else:
            return version("cog")
        import pkg_resources

        return pkg_resources.get_distribution("cog").version
    except Exception:
        return "unknown"


def get_user_agent() -> str:
    return f"cog-worker/{_get_version()}"
