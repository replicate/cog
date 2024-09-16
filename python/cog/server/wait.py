import os
import threading

from watchdog.observers import Observer

from .watch_handler import WatchHandler

COG_WAIT_FILE_ENV_VAR = "COG_WAIT_FILE"


def wait_for_file(timeout: float = 5.0) -> bool:
    """Wait for a file in the environment variables."""
    wait_file = os.environ.get(COG_WAIT_FILE_ENV_VAR)
    if wait_file is None:
        return True
    file_created_event = threading.Event()
    event_handler = WatchHandler(wait_file, file_created_event)
    observer = Observer()
    observer.schedule(event_handler, path=wait_file, recursive=False)
    observer.start()
    try:
        if os.path.exists(wait_file):
            return True
        file_created_event.wait(timeout)
        if file_created_event.is_set():
            return True
        return False
    finally:
        observer.stop()
        observer.join()
