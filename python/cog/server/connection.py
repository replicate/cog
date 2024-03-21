import asyncio
import io
import os
import socket
import struct
from multiprocessing import connection
from multiprocessing.connection import Connection
from typing import Any, Generic, TypeVar

X = TypeVar("X")
_ForkingPickler = connection._ForkingPickler


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
        # make the pipe bigger probably
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
                self._writer.write(header)
                self._writer.write(buf)
            else:
                self._writer.write(header + buf)

    def send(self, obj: Any) -> None:
        self._send_bytes(_ForkingPickler.dumps(obj, protocol=5))

    def close(self) -> None:
        self.wrapped_conn.close()
        self._writer.close()

    def __enter__(self) -> "AsyncConnection[X]":
        return self

    def __exit__(self, exc_type: Any, exc_value: Any, exc_tb: Any) -> None:
        self.close()
