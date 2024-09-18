import threading

from watchdog.events import FileSystemEvent, FileSystemEventHandler


class WatchHandler(FileSystemEventHandler):
    """A handler for watching a files events."""

    def __init__(self, filename: str, threading_event: threading.Event) -> None:
        self.filename = filename
        self.threading_event = threading_event

    def on_any_event(self, event: FileSystemEvent) -> None:
        if str(event.src_path).endswith(self.filename):
            self.threading_event.set()
