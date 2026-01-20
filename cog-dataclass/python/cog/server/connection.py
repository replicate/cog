import abc
import asyncio
import collections.abc
import multiprocessing
from multiprocessing.connection import Connection
from typing import Any, Optional

# Buffer is only available in typing-extensions>=4.6.0 but should be available in stdlib
# python 3.12+. This compatibility code is nearly identical to the implementation in
# typing-extensions>=4.6.0
if hasattr(collections.abc, "Buffer"):
    Buffer = collections.abc.Buffer  # type: ignore
else:

    class Buffer(abc.ABC):  # noqa: B024
        pass

    Buffer.register(memoryview)
    Buffer.register(bytearray)
    Buffer.register(bytes)

_spawn = multiprocessing.get_context("spawn")


class AsyncConnection:
    def __init__(self, connection: Connection) -> None:
        self._connection = connection
        self._event = asyncio.Event()
        loop = asyncio.get_event_loop()
        loop.add_reader(self._connection.fileno(), self._event.set)

    def send(self, obj: Any) -> None:
        """Send a (picklable) object"""

        self._connection.send(obj)

    async def _wait_for_input(self) -> None:
        """Wait until there is an input available to be read"""

        while not self._connection.poll():
            await self._event.wait()
            self._event.clear()

    async def recv(self) -> Any:
        """Receive a (picklable) object"""

        await self._wait_for_input()
        return self._connection.recv()

    def fileno(self) -> int:
        """File descriptor or handle of the connection"""
        return self._connection.fileno()

    def close(self) -> None:
        """Close the connection"""
        self._connection.close()

    async def poll(self, timeout: float = 0.0) -> bool:
        """Whether there is an input available to be read"""

        if self._connection.poll():
            return True

        try:
            await asyncio.wait_for(self._wait_for_input(), timeout=timeout)
        except asyncio.TimeoutError:
            return False
        return self._connection.poll()

    def send_bytes(
        self, buf: Buffer, offset: int = 0, size: Optional[int] = None
    ) -> None:
        """Send the bytes data from a bytes-like object"""

        self._connection.send_bytes(buf, offset, size)  # type: ignore

    async def recv_bytes(self, maxlength: Optional[int] = None) -> bytes:
        """
        Receive bytes data as a bytes object.
        """

        await self._wait_for_input()
        return self._connection.recv_bytes(maxlength)

    async def recv_bytes_into(self, buf: Buffer, offset: int = 0) -> int:
        """
        Receive bytes data into a writeable bytes-like object.
        Return the number of bytes read.
        """

        await self._wait_for_input()
        return self._connection.recv_bytes_into(buf, offset)


class LockedConnection:
    def __init__(self, connection: Connection) -> None:
        self.connection = connection
        self._lock = _spawn.Lock()

    def send(self, obj: Any) -> None:
        with self._lock:
            self.connection.send(obj)

    def recv(self) -> Any:
        return self.connection.recv()
