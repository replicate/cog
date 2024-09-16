import os
import tempfile
import threading
import time
from pathlib import Path

from cog.server.wait import COG_WAIT_FILE_ENV_VAR, wait_for_file


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
    os.environ[COG_WAIT_FILE_ENV_VAR] = "/a_file_unknown"
    result = wait_for_file(timeout=5.0)
    del os.environ[COG_WAIT_FILE_ENV_VAR]
    assert not result, "We should return false when the timeout triggers."
