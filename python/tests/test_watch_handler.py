import os
import threading
import time
from pathlib import Path

from watchdog.observers import Observer

from cog.watch_handler import WatchHandler


def test_watchhandler_signals_on_path():
    wait_file = os.path.join(os.path.dirname(__file__), "watchhandler_flag_file")
    if os.path.exists(wait_file):
        os.remove(wait_file)
    dir_path = os.path.dirname(wait_file)
    file_created_event = threading.Event()
    event_handler = WatchHandler(wait_file, file_created_event)
    observer = Observer()
    observer.schedule(event_handler, path=dir_path, recursive=True)
    observer.start()
    time.sleep(1.0)
    Path(wait_file).touch()
    time.sleep(1.0)
    os.remove(wait_file)
    assert file_created_event.is_set(), "File created threading event should be set"


def test_watchhandler_no_signal_for_wrong_path():
    wait_file = os.path.join(os.path.dirname(__file__), "watchhandler_flag_file_2")
    if os.path.exists(wait_file):
        os.remove(wait_file)
    watch_file = os.path.join(os.path.dirname(__file__), "another_flag_file")
    dir_path = os.path.dirname(wait_file)
    file_created_event = threading.Event()
    event_handler = WatchHandler(watch_file, file_created_event)
    observer = Observer()
    observer.schedule(event_handler, path=dir_path, recursive=True)
    observer.start()
    time.sleep(2.0)
    Path(wait_file).touch()
    os.remove(wait_file)
    assert (
        not file_created_event.is_set()
    ), "File created threading event should not be set"
