import asyncio
import io
import os
import socket
import struct
from multiprocessing import connection
from multiprocessing.connection import Connection
from typing import Any, Generic, TypeVar

X = TypeVar("X")
_ForkingPickler = connection._ForkingPickler  # type: ignore

# based on https://github.com/python/cpython/blob/main/Lib/multiprocessing/connection.py#L364


class AsyncConnection(Generic[X]):
    def __init__(self, conn: Connection) -> None:
        self.wrapped_conn = conn
        self.started = False

    async def async_init(self) -> None:
        fd = self.wrapped_conn.fileno()
        # mp may have handled something already but let's dup so exit is clean
        dup_fd = os.dup(fd)
        sock = socket.socket(fileno=dup_fd)
        sock.setblocking(False)
        # TODO: use /proc/sys/net/core/rmem_max, but special-case language models
        sz = 65536
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, sz)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, sz)
        self._reader, self._writer = await asyncio.open_connection(sock=sock)
        self.started = True

    async def _recv(self, size: int) -> io.BytesIO:
        if not self.started:
            await self.async_init()
        buf = io.BytesIO()
        remaining = size
        while remaining > 0:
            chunk = await self._reader.read(remaining)
            n = len(chunk)
            if n == 0:
                if remaining == size:
                    raise EOFError
                else:
                    raise OSError("got end of file during message")
            buf.write(chunk)
            remaining -= n
        return buf

    async def _recv_bytes(self) -> io.BytesIO:
        buf = await self._recv(4)
        (size,) = struct.unpack("!i", buf.getvalue())
        if size == -1:
            buf = await self._recv(8)
            (size,) = struct.unpack("!Q", buf.getvalue())
        return await self._recv(size)

    async def recv(self) -> X:
        buf = await self._recv_bytes()
        return _ForkingPickler.loads(buf.getbuffer())

    def _send_bytes(self, buf: bytes) -> None:
        n = len(buf)
        if n > 0x7FFFFFFF:
            pre_header = struct.pack("!i", -1)
            header = struct.pack("!Q", n)
            self._writer.write(pre_header)
            self._writer.write(header)
            self._writer.write(buf)
        else:
            header = struct.pack("!i", n)
            if n > 16384:
                # >The payload is large so Nagle's algorithm won't be triggered
                # >and we'd better avoid the cost of concatenation.
                self._writer.write(header)
                self._writer.write(buf)
            else:
                # >Issue #20540: concatenate before sending, to avoid delays due
                # >to Nagle's algorithm on a TCP socket.
                # >Also note we want to avoid sending a 0-length buffer separately,
                # >to avoid "broken pipe" errors if the other end closed the pipe.
                self._writer.write(header + buf)

    def send(self, obj: Any) -> None:
        self._send_bytes(_ForkingPickler.dumps(obj, protocol=5))

    # we could implement async def drain() but it's not really necessary for our purposes

    def close(self) -> None:
        self.wrapped_conn.close()
        self._writer.close()

    def __enter__(self) -> "AsyncConnection[X]":
        return self

    def __exit__(self, exc_type: Any, exc_value: Any, exc_tb: Any) -> None:
        self.close()
