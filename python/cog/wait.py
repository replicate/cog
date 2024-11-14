import importlib
import os
import sys
import time

import structlog

COG_WAIT_FILE_ENV_VAR = "COG_WAIT_FILE"
COG_EAGER_IMPORTS_ENV_VAR = "COG_EAGER_IMPORTS"
COG_PYENV_PATH_ENV_VAR = "COG_PYENV_PATH"
PYTHONPATH_ENV_VAR = "PYTHONPATH"
PYTHON_VERSION_ENV_VAR = "R8_PYTHON_VERSION"

log = structlog.get_logger("cog.wait")


def _wait_flag_fallen() -> bool:
    wait_file = os.environ.get(COG_WAIT_FILE_ENV_VAR)
    if wait_file is None:
        return True
    return os.path.exists(wait_file)


def _insert_pythonpath() -> None:
    pyenv_path = os.environ.get(COG_PYENV_PATH_ENV_VAR)
    if pyenv_path is None:
        return
    full_module_path = os.path.join(
        pyenv_path,
        "lib",
        "python" + os.environ[PYTHON_VERSION_ENV_VAR],
        "site-packages",
    )
    if full_module_path not in sys.path:
        sys.path.append(full_module_path)
    os.environ[PYTHONPATH_ENV_VAR] = ":".join(sys.path)


def wait_for_file(timeout: float = 60.0) -> bool:
    """Wait for a file in the environment variables."""
    wait_file = os.environ.get(COG_WAIT_FILE_ENV_VAR)
    if wait_file is None:
        return True
    if os.path.exists(wait_file):
        log.info(f"Wait file found {wait_file}...")
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
        _insert_pythonpath()
        return True
    if include_imports:
        eagerly_import_modules()
    waited = wait_for_file(timeout=file_timeout)
    _insert_pythonpath()
    return waited
