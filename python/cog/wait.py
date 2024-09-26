import importlib
import os
import time

import structlog

COG_WAIT_FILE_ENV_VAR = "COG_WAIT_FILE"
COG_EAGER_IMPORTS_ENV_VAR = "COG_EAGER_IMPORTS"

log = structlog.get_logger("cog.wait")


def _wait_flag_fallen() -> bool:
    wait_file = os.environ.get(COG_WAIT_FILE_ENV_VAR)
    if wait_file is None:
        return True
    return os.path.exists(wait_file)


def wait_for_file(timeout: float = 60.0) -> bool:
    """Wait for a file in the environment variables."""
    wait_file = os.environ.get(COG_WAIT_FILE_ENV_VAR)
    if wait_file is None:
        return True
    if os.path.exists(wait_file):
        return True
    log.info(f"Waiting for file {wait_file}...")
    time_taken = 0.0
    while time_taken < timeout:
        sleep_time = 0.01
        time.sleep(sleep_time)
        time_taken += sleep_time
        if os.path.exists(wait_file):
            return True
    log.info(f"Waiting for file {wait_file} timed out.")
    return False


def eagerly_import_modules() -> int:
    """Wait for python to import big modules."""
    wait_imports = os.environ.get(COG_EAGER_IMPORTS_ENV_VAR)
    import_count = 0
    if wait_imports is None:
        return import_count
    log.info(f"Eagerly importing {wait_imports}.")
    for import_statement in wait_imports.split(","):
        importlib.import_module(import_statement)
        import_count += 1
    return import_count


def wait_for_env(file_timeout: float = 60.0, include_imports: bool = True) -> bool:
    """Wait for the environment to load."""
    if _wait_flag_fallen():
        return True
    if include_imports:
        eagerly_import_modules()
    return wait_for_file(timeout=file_timeout)
