from cog import current_scope


def run() -> dict[str, str]:
    return current_scope().context
