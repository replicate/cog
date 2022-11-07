import os
import selectors
import sys
import threading
import time
import uuid

from .eventtypes import Log


class WrappedStream:
    def __init__(self, name, stream):
        self.name = name
        self._stream = stream
        self._original_fp = None
        self._wrapped_fp = None

    def wrap(self):
        r, w = os.pipe()

        # Save a copy of the original stream file descriptor.
        original_fd = self._stream.fileno()
        original_fd_copy = os.dup(original_fd)

        # Overwrite the original file descriptor with the write end of the
        # pipe.
        os.dup2(w, original_fd)

        # Create a writeable file object with the original FD. This can be used
        # to write to the original destination of the passed stream.
        self._original_fp = os.fdopen(original_fd_copy, "w")

        # Create a readable file object with the read end of the pipe. This can
        # be used to read any writes to the passed stream.
        #
        # We set the FD to be non-blocking so that we can select/poll/epoll
        # over multiple wrapped streams.
        os.set_blocking(r, False)
        self._wrapped_fp = os.fdopen(r, "r", buffering=1)

    @property
    def wrapped(self):
        if not self._wrapped_fp:
            raise RuntimeError("you must call wrap() before using wrapped")
        return self._wrapped_fp

    @property
    def original(self):
        if not self._original_fp:
            raise RuntimeError("you must call wrap() before using original")
        return self._original_fp


class StreamRedirector(threading.Thread):
    """
    StreamRedirector captures data passing through the STDOUT and STDERR I/O
    streams, and copies each line onto `events`, a
    :py:class:`multiprocessing.connection.Connection` object.

    It also passes the data through to the original stream.
    """

    def __init__(self, events):
        self.events = events
        self.stdout = WrappedStream("stdout", sys.stdout)
        self.stderr = WrappedStream("stderr", sys.stderr)
        self.drain_token = str(uuid.uuid4())
        self.drain_event = threading.Event()
        self.terminate_token = str(uuid.uuid4())

        # Setting daemon=True ensures that threading._shutdown will not wait
        # for this thread if a fatal exception (SystemExit, KeyboardInterrupt)
        # occurs.
        #
        # Or, to put it another way, it ensures that if this is the only thread
        # still running, Python will exit.
        super().__init__(daemon=True)

    def redirect(self):
        self.stdout.wrap()
        self.stderr.wrap()

    def drain(self):
        self.drain_event.clear()
        print(self.drain_token, flush=True)
        print(self.drain_token, file=sys.stderr, flush=True)
        if not self.drain_event.wait(timeout=1):
            raise RuntimeError("output streams failed to drain")

    def shutdown(self):
        print(self.terminate_token, flush=True)
        self.join()

    def run(self):
        selector = selectors.DefaultSelector()

        should_exit = False
        stdout_key = selector.register(self.stdout.wrapped, selectors.EVENT_READ, self.stdout)
        stderr_key = selector.register(self.stderr.wrapped, selectors.EVENT_READ, self.stderr)
        drain_tokens_seen = 0
        drain_tokens_needed = 2

        while not should_exit:
            for key, _ in selector.select():
                stream = key.data

                for line in key.fileobj:

                    if line.strip() == self.terminate_token:
                        should_exit = True
                        continue

                    if line.strip() == self.drain_token:
                        drain_tokens_seen += 1

                        if drain_tokens_seen >= drain_tokens_needed:
                            self.drain_event.set()
                            drain_tokens_seen = 0

                        continue

                    stream.original.write(line)
                    self.events.send(Log(line, source=stream.name))
