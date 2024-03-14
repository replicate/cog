import asyncio
import concurrent.futures
import io
import os
import selectors
import sys
import threading
import uuid
from multiprocessing.connection import Connection
from typing import (
    Any,
    Callable,
    Coroutine,
    Generic,
    Optional,
    Sequence,
    TextIO,
    TypeVar,
    Union,
)


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
        self.drain_event.clear()
        for stream in self._streams:
            stream.write(self.drain_token + "\n")
            stream.flush()
        if not self.drain_event.wait(timeout=1):
            raise RuntimeError("output streams failed to drain")

    def shutdown(self) -> None:
        for stream in self._streams:
            stream.write(self.terminate_token + "\n")
            stream.flush()
            break  # only need to write one terminate token
        self.join()

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
                        self._write_hook(stream.name, stream.original, full_line + "\n")

                    if drain_tokens_seen >= drain_tokens_needed:
                        self.drain_event.set()
                        drain_tokens_seen = 0


X = TypeVar("X")
Y = TypeVar("Y")


async def race(
    x: Coroutine[None, None, X],
    y: Coroutine[None, None, Y],
    timeout: Optional[float] = None,
) -> Union[X, Y]:
    tasks = [asyncio.create_task(x), asyncio.create_task(y)]
    wait = asyncio.wait(tasks, timeout=timeout, return_when=asyncio.FIRST_COMPLETED)
    done, pending = await wait
    for task in pending:
        task.cancel()
    if not done:
        raise TimeoutError
    # done is an unordered set but we want to preserve original order
    result_task, *others = (t for t in tasks if t in done)
    # during shutdown, some of the other completed tasks might be an error
    # cancel them instead of handling the error to avoid the warning
    # "Task exception was never retrieved"
    for task in others:
        msg = "was completed at the same time as another selected task, canceling"
        # FIXME: ues a logger?
        print(task, msg, file=sys.stderr)
        task.cancel()
    return result_task.result()


# functionally this is the exact same thing as aioprocessing but 0.1% the code
# however it's still worse than just using actual asynchronous io
class AsyncPipe(Generic[X]):
    def __init__(
        self, conn: Connection, alive: Callable[[], bool] = lambda: True
    ) -> None:
        self.conn = conn
        self.alive = alive
        self.exiting = threading.Event()
        self.executor = concurrent.futures.ThreadPoolExecutor(1)

    def send(self, obj: Any) -> None:
        self.conn.send(obj)

    def shutdown(self) -> None:
        self.exiting.set()
        self.executor.shutdown(wait=True)
        # if we ever need cancel_futures (introduced 3.9), we can copy it in from
        # https://github.com/python/cpython/blob/3.11/Lib/concurrent/futures/thread.py#L216-L235

    def poll(self, timeout: float = 0.0) -> bool:
        return self.conn.poll(timeout)

    def _recv(self) -> Optional[X]:
        # this ugly mess could easily be avoided with loop.connect_read_pipe
        # even loop.add_reader would help but we don't want to mess with a thread-local loop
        while not self.exiting.is_set() and not self.conn.closed and self.alive():
            if self.conn.poll(0.01):
                if self.conn.closed or not self.alive():
                    print("caught conn closed or unalive")
                    return
                return self.conn.recv()
        return None

    async def coro_recv(self) -> Optional[X]:
        loop = asyncio.get_running_loop()
        # believe it or not this can still deadlock!
        return await loop.run_in_executor(self.executor, self._recv)

    async def coro_recv_with_exit(self, exit: asyncio.Event) -> Optional[X]:
        result = await race(self.coro_recv(), exit.wait())
        if result is not True:  # wait() would return True
            return result
