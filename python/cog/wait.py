import importlib
import os
import threading

from watchdog.observers import Observer

from .watch_handler import WatchHandler

COG_WAIT_FILE_ENV_VAR = "COG_WAIT_FILE"
COG_EAGER_IMPORTS_ENV_VAR = "COG_EAGER_IMPORTS"


def wait_for_file(timeout: float = 60.0) -> bool:
    """Wait for a file in the environment variables."""
    wait_file = os.environ.get(COG_WAIT_FILE_ENV_VAR)
    if wait_file is None:
        return True
    dir_path = os.path.dirname(wait_file)
    os.makedirs(dir_path, exist_ok=True)
    file_created_event = threading.Event()
    event_handler = WatchHandler(wait_file, file_created_event)
    observer = Observer()
    observer.schedule(event_handler, path=dir_path, recursive=True)
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


def eagerly_import_modules() -> int:
    """Wait for python to import big modules."""
    wait_imports = os.environ.get(COG_EAGER_IMPORTS_ENV_VAR)
    import_count = 0
    if wait_imports is None:
        return import_count
    for import_statement in wait_imports.split(","):
        importlib.import_module(import_statement)
        import_count += 1
    return import_count


def wait_for_env(file_timeout: float = 60.0, include_imports: bool = True) -> bool:
    """Wait for the environment to load."""
    if include_imports:
        eagerly_import_modules()
    return wait_for_file(timeout=file_timeout)
