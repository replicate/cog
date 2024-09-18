import os
import tempfile
import threading
import time
from pathlib import Path

from cog.wait import (
    COG_WAIT_FILE_ENV_VAR,
    COG_WAIT_IMPORTS_ENV_VAR,
    wait_for_env,
    wait_for_file,
    wait_for_imports,
)


def test_wait_for_file_no_env_var():
    if COG_WAIT_FILE_ENV_VAR in os.environ:
        del os.environ[COG_WAIT_FILE_ENV_VAR]
    result = wait_for_file()
    assert result, "We should immediately return when no wait file is specified."


def test_wait_for_file_exists():
    with tempfile.NamedTemporaryFile() as tmpfile:
        os.environ[COG_WAIT_FILE_ENV_VAR] = tmpfile.name
        result = wait_for_file()
        del os.environ[COG_WAIT_FILE_ENV_VAR]
        assert result, "We should immediately return when the file already exists."


def test_wait_for_file_waits_for_file():
    wait_file = os.path.join(os.path.dirname(__file__), "flag_file")
    if os.path.exists(wait_file):
        os.remove(wait_file)
    os.environ[COG_WAIT_FILE_ENV_VAR] = wait_file

    def create_file():
        time.sleep(2.0)
        Path(wait_file).touch()

    thread = threading.Thread(target=create_file)
    thread.start()
    result = wait_for_file(timeout=5.0)
    del os.environ[COG_WAIT_FILE_ENV_VAR]
    os.remove(wait_file)
    assert result, "We should return when the file is touched."


def test_wait_for_file_timeout():
    os.environ[COG_WAIT_FILE_ENV_VAR] = os.path.join(
        os.path.dirname(__file__), "a_file_unknown"
    )
    result = wait_for_file(timeout=5.0)
    del os.environ[COG_WAIT_FILE_ENV_VAR]
    assert not result, "We should return false when the timeout triggers."


def test_wait_for_imports_no_env_var():
    if COG_WAIT_IMPORTS_ENV_VAR in os.environ:
        del os.environ[COG_WAIT_IMPORTS_ENV_VAR]
    wait_for_imports()


def test_wait_for_imports():
    os.environ[COG_WAIT_IMPORTS_ENV_VAR] = "pytest,pathlib,time"
    import_count = wait_for_imports()
    del os.environ[COG_WAIT_IMPORTS_ENV_VAR]
    assert import_count == 3, "There should be 3 imports performed"


def test_wait_for_env_no_env_vars():
    if COG_WAIT_FILE_ENV_VAR in os.environ:
        del os.environ[COG_WAIT_FILE_ENV_VAR]
    if COG_WAIT_IMPORTS_ENV_VAR in os.environ:
        del os.environ[COG_WAIT_IMPORTS_ENV_VAR]
    result = wait_for_env()
    assert (
        result
    ), "We should return true if we have no env vars associated with the wait."


def test_wait_for_env():
    with tempfile.NamedTemporaryFile() as tmpfile:
        os.environ[COG_WAIT_FILE_ENV_VAR] = tmpfile.name
        os.environ[COG_WAIT_IMPORTS_ENV_VAR] = "pytest,pathlib,time"
        result = wait_for_env()
        assert (
            result
        ), "We should return true if we have waited for the right environment."
        del os.environ[COG_WAIT_IMPORTS_ENV_VAR]
        del os.environ[COG_WAIT_FILE_ENV_VAR]
