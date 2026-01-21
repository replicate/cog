"""
Helper utilities for the Cog server.
"""

from __future__ import annotations

import contextlib
import io
import os
import selectors
import sys
import threading
import uuid
from types import TracebackType
from typing import (
    BinaryIO,
    Callable,
    Dict,
    List,
    Sequence,
    TextIO,
)

from typing_extensions import Self

from .errors import CogRuntimeError, CogTimeoutError


class _SimpleStreamWrapper(io.TextIOWrapper):
    """
    _SimpleStreamWrapper wraps a binary I/O buffer and provides a TextIOWrapper
    interface (primarily write and flush methods) which call a provided
    callback function instead of (or, if `tee` is True, in addition to) writing
    to the underlying buffer.
    """

    def __init__(
        self,
        buffer: BinaryIO,
        callback: Callable[[str, str], None],
        tee: bool = False,
    ) -> None:
        super().__init__(buffer)

        self._callback = callback
        self._tee = tee
        self._buffer: List[str] = []

    def write(self, s: str) -> int:
        length = len(s)
        self._buffer.append(s)
        if self._tee:
            super().write(s)

        if "\n" in s or "\r" in s:
            self.flush()

        return length

    def flush(self) -> None:
        self._callback(self.name, "".join(self._buffer))
        self._buffer.clear()
        if self._tee:
            super().flush()


class _StreamWrapper:
    def __init__(self, name: str, stream: TextIO) -> None:
        self.name = name
        self._stream = stream
        self._original_fp: TextIO | None = None
        self._wrapped_fp: TextIO | None = None

    def wrap(self) -> None:
        if self._wrapped_fp or self._original_fp:
            raise CogRuntimeError("stream is already wrapped")

        r, w = os.pipe()

        # Save a copy of the stream file descriptor.
        original_fd = self._stream.fileno()
        original_fd_copy = os.dup(original_fd)

        # Overwrite the stream file descriptor with the write end of the pipe.
        os.dup2(w, self._stream.fileno())
        os.close(w)

        # Create a writeable file object with the original FD. This can be used
        # to write to the original destination of the passed stream.
        self._original_fp = os.fdopen(original_fd_copy, "w")

        # Create a readable file object with the read end of the pipe. This can
        # be used to read any writes to the passed stream.
        #
        # We set the FD to be non-blocking so that we can select/poll/epoll
        # over multiple wrapped streams.
        os.set_blocking(r, False)
        self._wrapped_fp = os.fdopen(r, "r")

    def unwrap(self) -> None:
        if not self._wrapped_fp or not self._original_fp:
            raise CogRuntimeError("stream is not wrapped (call wrap first)")

        # Put the original file descriptor back.
        os.dup2(self._original_fp.fileno(), self._stream.fileno())

        # Close the write end of the pipe.
        self._original_fp.close()
        self._original_fp = None

        # Close the read end of the pipe.
        self._wrapped_fp.close()
        self._wrapped_fp = None

    def write(self, data: str) -> int:
        return self._stream.write(data)

    def flush(self) -> None:
        return self._stream.flush()

    @property
    def wrapped(self) -> TextIO:
        if not self._wrapped_fp:
            raise CogRuntimeError("stream is not wrapped (call wrap first)")
        return self._wrapped_fp

    @property
    def original(self) -> TextIO:
        if not self._original_fp:
            raise CogRuntimeError("stream is not wrapped (call wrap first)")
        return self._original_fp


if sys.version_info < (3, 9):

    class _SimpleStreamRedirectorBase(contextlib.AbstractContextManager):
        pass
else:

    class _SimpleStreamRedirectorBase(
        contextlib.AbstractContextManager["SimpleStreamRedirector"]
    ):
        pass


class SimpleStreamRedirector(_SimpleStreamRedirectorBase):
    """
    SimpleStreamRedirector is a context manager that redirects I/O streams to a
    callback function. If `tee` is True, it also writes output to the original
    streams.

    Unlike StreamRedirector, the underlying stream file descriptors are not
    modified, which means that only stream writes from Python code will be
    captured. Writes from native code will not be captured.

    Unlike StreamRedirector, the streams redirected cannot be configured. The
    context manager is only able to redirect STDOUT and STDERR.
    """

    def __init__(
        self,
        callback: Callable[[str, str], None],
        tee: bool = False,
    ) -> None:
        self._callback = callback
        self._tee = tee

        stdout_wrapper = _SimpleStreamWrapper(sys.stdout.buffer, callback, tee)
        stderr_wrapper = _SimpleStreamWrapper(sys.stderr.buffer, callback, tee)
        self._stdout_ctx = contextlib.redirect_stdout(stdout_wrapper)
        self._stderr_ctx = contextlib.redirect_stderr(stderr_wrapper)

    def __enter__(self) -> Self:
        self._stdout_ctx.__enter__()
        self._stderr_ctx.__enter__()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> None:
        self._stdout_ctx.__exit__(exc_type, exc_value, traceback)
        self._stderr_ctx.__exit__(exc_type, exc_value, traceback)

    def drain(self, timeout: float = 0.0) -> None:
        # Draining isn't complicated for SimpleStreamRedirector, since we're not
        # moving data between threads. We just need to flush the streams.
        sys.stdout.flush()
        sys.stderr.flush()


if sys.version_info < (3, 9):

    class _StreamRedirectorBase(contextlib.AbstractContextManager):
        pass
else:

    class _StreamRedirectorBase(contextlib.AbstractContextManager["StreamRedirector"]):
        pass


class StreamRedirector(_StreamRedirectorBase):
    """
    StreamRedirector is a context manager that redirects I/O streams to a
    callback function. If `tee` is True, it also writes output to the original
    streams.

    If `streams` is not provided, it defaults to redirecting the process's
    STDOUT and STDERR file descriptors.
    """

    def __init__(
        self,
        callback: Callable[[str, str], None],
        tee: bool = False,
        streams: Sequence[TextIO] | None = None,
    ) -> None:
        self._callback = callback
        self._tee = tee

        self._depth = 0
        self._drain_token = uuid.uuid4().hex
        self._drain_event = threading.Event()
        self._terminate_token = uuid.uuid4().hex

        if not streams:
            streams = [sys.stdout, sys.stderr]
        self._streams = [_StreamWrapper(s.name, s) for s in streams]

    def __enter__(self) -> Self:
        self._depth += 1

        if self._depth == 1:
            for s in self._streams:
                s.wrap()

            self._thread = threading.Thread(target=self._start)
            self._thread.start()

        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> None:
        self._depth -= 1

        if self._depth == 0:
            self._stop()
            self._thread.join()

            for s in self._streams:
                s.unwrap()

    def drain(self, timeout: float = 1) -> None:
        self._drain_event.clear()
        for stream in self._streams:
            stream.write(self._drain_token + "\n")
            stream.flush()
        if not self._drain_event.wait(timeout=timeout):
            raise CogTimeoutError("output streams failed to drain")

    def _start(self) -> None:
        selector = selectors.DefaultSelector()

        should_exit = False
        drain_tokens_seen = 0
        drain_tokens_needed = 0
        buffers: Dict[str, io.StringIO] = {}

        for stream in self._streams:
            selector.register(stream.wrapped, selectors.EVENT_READ, stream)
            buffers[stream.name] = io.StringIO()
            drain_tokens_needed += 1

        while not should_exit:
            for key, _ in selector.select():
                stream = key.data

                for line in stream.wrapped:
                    if not line.endswith("\n"):
                        # TODO: limit how much we're prepared to buffer on a
                        # single line
                        buffers[stream.name].write(line)
                        continue

                    full_line = buffers[stream.name].getvalue() + line.strip()

                    # Reset buffer (this is quicker and easier than resetting
                    # the existing buffer, but may generate more garbage).
                    buffers[stream.name] = io.StringIO()

                    if full_line.endswith(self._terminate_token):
                        should_exit = True
                        full_line = full_line[: -len(self._terminate_token)]

                    if full_line.endswith(self._drain_token):
                        drain_tokens_seen += 1
                        full_line = full_line[: -len(self._drain_token)]

                    # If full_line is empty at this point it means the only
                    # thing in the line was a drain token (or a terminate
                    # token).
                    if full_line:
                        self._callback(stream.name, full_line + "\n")
                        if self._tee:
                            stream.original.write(full_line + "\n")
                            stream.original.flush()

                    if drain_tokens_seen >= drain_tokens_needed:
                        self._drain_event.set()
                        drain_tokens_seen = 0

    def _stop(self) -> None:
        for s in self._streams:
            s.write(self._terminate_token + "\n")
            s.flush()
            break  # we only need to send the terminate token to one stream
