import asyncio
import io
import os
import selectors
import threading
import uuid
from typing import Callable, Optional, Sequence, TextIO


def debug(*args: str, f: io.IOBase = open("/tmp/debug", "a")) -> None:  # noqa
    print(*args, file=f, flush=True)


async def async_fdopen(fd: int) -> asyncio.StreamReader:
    loop = asyncio.get_running_loop()
    reader = asyncio.StreamReader()
    protocol = asyncio.StreamReaderProtocol(reader)
    loop.create_task(loop.connect_read_pipe(lambda: protocol, os.fdopen(fd, "rb")))
    return reader


class WrappedStream:
    def __init__(self, name: str, stream: TextIO) -> None:
        self.name = name
        self._stream = stream
        self._original_fp: Optional[TextIO] = None
        self._wrapped_fp: Optional[TextIO] = None

    def wrap(self) -> None:
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
        self._wrapped_fp = os.fdopen(r, "r")

    def write(self, data: str) -> int:
        return self._stream.write(data)

    def flush(self) -> None:
        return self._stream.flush()

    @property
    def wrapped(self) -> TextIO:
        if not self._wrapped_fp:
            raise RuntimeError("you must call wrap() before using wrapped")
        return self._wrapped_fp

    @property
    def original(self) -> TextIO:
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

    def __init__(
        self,
        streams: Sequence[WrappedStream],
        write_hook: Callable[[str, TextIO, str], None],
    ) -> None:
        self._streams = list(streams)
        self._write_hook = write_hook
        self.drain_token = uuid.uuid4().hex
        self.drain_event = threading.Event()
        self.terminate_token = uuid.uuid4().hex
        self.is_async = False

        if len(self._streams) == 0:
            raise ValueError("provide at least one wrapped stream to redirect")

        # Setting daemon=True ensures that threading._shutdown will not wait
        # for this thread if a fatal exception (SystemExit, KeyboardInterrupt)
        # occurs.
        #
        # Or, to put it another way, it ensures that if this is the only thread
        # still running, Python will exit.
        super().__init__(daemon=True)

    def drain(self) -> None:
        if self.is_async:
            debug("ignoring drain")
            # if we're async, we assume that logs will be processed promptly,
            # and we don't want to block the event loop
            return
        self.drain_event.clear()
        for stream in self._streams:
            debug(repr(stream), stream.name)
            stream.write(self.drain_token + "\n")
            debug(repr(stream), "flush")
            stream.flush()
        debug("wait drain")
        if not self.drain_event.wait(timeout=1):
            debug("drain timed out")
            raise RuntimeError("output streams failed to drain")
        debug("drain done")

    def shutdown(self) -> None:
        if not self.is_alive():
            debug("skipping shutdown because not alive")
            return
        for stream in self._streams:
            stream.write(self.terminate_token + "\n")
            stream.flush()
            break  # only need to write one terminate token
        debug("joining")
        self.join()

    async def shutdown_async(self) -> None:
        for stream in self._streams:
            stream.write(self.terminate_token + "\n")
            stream.flush()
        await asyncio.gather(*self.stream_tasks)

    def run(self) -> None:
        selector = selectors.DefaultSelector()

        should_exit = False
        drain_tokens_seen = 0
        drain_tokens_needed = 0
        buffers = {}

        for stream in self._streams:
            selector.register(stream.wrapped, selectors.EVENT_READ, stream)
            buffers[stream.name] = io.StringIO()
            drain_tokens_needed += 1

        while not should_exit:
            debug("selector.select")
            for key, _ in selector.select():
                debug("selector key")
                stream = key.data

                for line in stream.wrapped:
                    debug("redirector saw", line)
                    if not line.endswith("\n"):
                        # TODO: limit how much we're prepared to buffer on a
                        # single line
                        buffers[stream.name].write(line)
                        continue

                    full_line = buffers[stream.name].getvalue() + line.strip()

                    # Reset buffer (this is quicker and easier than resetting
                    # the existing buffer, but may generate more garbage).
                    buffers[stream.name] = io.StringIO()

                    if full_line.endswith(self.terminate_token):
                        should_exit = True
                        full_line = full_line[: -len(self.terminate_token)]

                    if full_line.endswith(self.drain_token):
                        drain_tokens_seen += 1
                        full_line = full_line[: -len(self.drain_token)]

                    # If full_line is emptry at this point it means the only
                    # thing in the line was a drain token (or a terminate
                    # token).
                    if full_line:
                        debug("write hook")
                        self._write_hook(stream.name, stream.original, full_line + "\n")
                        debug("write hook done")

                    if drain_tokens_seen >= drain_tokens_needed:
                        debug("drain event set")
                        self.drain_event.set()
                        drain_tokens_seen = 0

    async def switch_to_async(self) -> None:
        """
        This function is called when the main thread switches to being async.
        It ensures that the behavior stays the same, but write_hook is only called
        from the main thread.

        1. Open each stream as a StreamReader.
        2. Create a task for each stream that will process the results.
        3. write_hook is called for each complete log line.
        4. Drain and terminate tokens are handled correctly.
        5. Once the async tasks are started, shut down the thread.

        We must not call write_hook twice for the same data during the switch.
        """
        debug("switch async, drain")
        # Drain the streams to ensure all buffered data is processed
        try:
            self.drain()
        except RuntimeError:
            debug("drain failed")
            raise
        debug("drain done, shutdown")

        # Shut down the thread
        # we do this before starting a coroutine that will also read from the same fd
        # so that shutdown can find the terminate tokens correctly
        self.shutdown()
        self.stream_tasks = []
        self.is_async = True
        debug("set is async")

        for stream in self._streams:
            # Open each stream as a StreamReader
            fd = stream.wrapped.fileno()
            reader = await async_fdopen(fd)

            # Create a task for each stream to process the results
            task = asyncio.create_task(self.process_stream(stream, reader))
            self.stream_tasks.append(task)

        # give the tasks a chance to start
        await asyncio.sleep(0)

    async def process_stream(
        self, stream: WrappedStream, reader: asyncio.StreamReader
    ) -> None:
        debug("process_stream", stream.name)
        buffer = io.StringIO()
        drain_tokens_seen = 0
        should_exit = False

        async for line in reader:

            if not line:
                break

            line = line.decode()
            debug("redirector saw", line)

            if not line.endswith("\n"):
                buffer.write(line)
                continue

            full_line = buffer.getvalue() + line.strip()

            # Reset buffer
            buffer = io.StringIO()

            if full_line.endswith(self.terminate_token):
                full_line = full_line[: -len(self.terminate_token)]
                should_exit = True

            if full_line.endswith(self.drain_token):
                drain_tokens_seen += 1
                full_line = full_line[: -len(self.drain_token)]

            if full_line:
                # Call write_hook from the main thread
                self._write_hook(stream.name, stream.original, full_line + "\n")

            if drain_tokens_seen >= len(self._streams):
                debug("drain event set")
                self.drain_event.set()
                drain_tokens_seen = 0
            if should_exit:
                break
