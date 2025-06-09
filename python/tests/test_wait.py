import os
import sys
import tempfile
import threading
import time
from pathlib import Path

from cog.wait import (
    COG_EAGER_IMPORTS_ENV_VAR,
    COG_PYENV_PATH_ENV_VAR,
    COG_WAIT_FILE_ENV_VAR,
    PYTHON_VERSION_ENV_VAR,
    PYTHONPATH_ENV_VAR,
    eagerly_import_modules,
    wait_for_env,
    wait_for_file,
)


def test_wait_for_file_no_env_var():
    if COG_WAIT_FILE_ENV_VAR in os.environ:
        del os.environ[COG_WAIT_FILE_ENV_VAR]
    result = wait_for_file()
    assert result, "We should immediately return when no wait file is specified."


def test_wait_for_file_exists():
    with tempfile.NamedTemporaryFile() as tmpfile:
        os.environ[COG_WAIT_FILE_ENV_VAR] = tmpfile.name
        result = wait_for_file(timeout=5.0)
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


def test_eagerly_import_modules_no_env_var():
    if COG_EAGER_IMPORTS_ENV_VAR in os.environ:
        del os.environ[COG_EAGER_IMPORTS_ENV_VAR]
    eagerly_import_modules()


def test_eagerly_import_modules():
    os.environ[COG_EAGER_IMPORTS_ENV_VAR] = "pytest,pathlib,time"
    import_count = eagerly_import_modules()
    del os.environ[COG_EAGER_IMPORTS_ENV_VAR]
    assert import_count == 3, "There should be 3 imports performed"


def test_wait_for_env_no_env_vars():
    if COG_WAIT_FILE_ENV_VAR in os.environ:
        del os.environ[COG_WAIT_FILE_ENV_VAR]
    if COG_EAGER_IMPORTS_ENV_VAR in os.environ:
        del os.environ[COG_EAGER_IMPORTS_ENV_VAR]
    result = wait_for_env()
    assert result, (
        "We should return true if we have no env vars associated with the wait."
    )


def test_wait_for_env():
    with tempfile.NamedTemporaryFile() as tmpfile:
        os.environ[COG_WAIT_FILE_ENV_VAR] = tmpfile.name
        os.environ[COG_EAGER_IMPORTS_ENV_VAR] = "pytest,pathlib,time"
        result = wait_for_env()
        assert result, (
            "We should return true if we have waited for the right environment."
        )
        del os.environ[COG_EAGER_IMPORTS_ENV_VAR]
        del os.environ[COG_WAIT_FILE_ENV_VAR]


def test_wait_inserts_pythonpath():
    with tempfile.NamedTemporaryFile() as tmpfile:
        original_sys_path = sys.path.copy()
        original_python_path = os.environ.get(PYTHONPATH_ENV_VAR)
        pyenv_path = os.path.dirname(tmpfile.name)
        os.environ[COG_WAIT_FILE_ENV_VAR] = tmpfile.name
        os.environ[COG_EAGER_IMPORTS_ENV_VAR] = "pytest,pathlib,time"
        os.environ[COG_PYENV_PATH_ENV_VAR] = pyenv_path
        os.environ[PYTHON_VERSION_ENV_VAR] = "3.11"
        wait_for_env()
        del os.environ[PYTHON_VERSION_ENV_VAR]
        del os.environ[COG_PYENV_PATH_ENV_VAR]
        del os.environ[COG_EAGER_IMPORTS_ENV_VAR]
        del os.environ[COG_WAIT_FILE_ENV_VAR]
        current_python_path = os.environ[PYTHONPATH_ENV_VAR]
        if original_python_path is None:
            del os.environ[PYTHONPATH_ENV_VAR]
        else:
            os.environ[PYTHONPATH_ENV_VAR] = original_python_path
        sys.path = original_sys_path
        expected_path = ":".join(
            original_sys_path + [pyenv_path + "/lib/python3.11/site-packages"]
        )
        assert expected_path == current_python_path, (
            "Our python path should be updated with the pyenv path."
        )
