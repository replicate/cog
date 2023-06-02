# channel.py
#
# Support for a message passing channel that can send bytes or pickled
# Python objects on a stream.  Compatible with the Connection class in the
# multiprocessing module, but rewritten for a purely asynchronous runtime.

__all__ = ['Channel']

# -- Standard Library

import os
import pickle
import struct
import hmac
import multiprocessing.connection as mpc
import logging

log = logging.getLogger(__name__)

# -- Curio

from . import socket
from .errors import CurioError, TaskTimeout
from .io import StreamBase, FileStream
from . import thread
from .time import timeout_after, sleep

# Authentication parameters (copied from multiprocessing)

AUTH_MESSAGE_LENGTH = mpc.MESSAGE_LENGTH    # 20
CHALLENGE = mpc.CHALLENGE                   # b'#CHALLENGE#'
WELCOME = mpc.WELCOME                       # b'#WELCOME#'
FAILURE = mpc.FAILURE                       # b'#FAILURE#'



class ConnectionError(CurioError):
    pass


class AuthenticationError(ConnectionError):
    pass


class Connection(object):
    '''
    A communication channel for sending size-prefixed messages of bytes
    or pickled Python objects.  Must be passed a pair of reader/writer
    streams for performing the underlying communication.
    '''

    def __init__(self, reader, writer):
        assert isinstance(reader, StreamBase) and isinstance(writer, StreamBase)
        self._reader = reader
        self._writer = writer

    @classmethod
    def from_Connection(cls, conn):
        '''
        Creates a channel from a multiprocessing Connection. Note: The
        multiprocessing connection is detached by having its handle set to None.

        This method can be used to make curio talk over Pipes as created by
        multiprocessing.  For example:

              p1, p2 = multiprocessing.Pipe()
              p1 = Connection.from_Connection(p1)
              p2 = Connection.from_Connection(p2)

        '''
        assert isinstance(conn, mpc._ConnectionBase)
        reader = FileStream(open(conn._handle, 'rb', buffering=0))
        writer = FileStream(open(conn._handle, 'wb', buffering=0, closefd=False))
        conn._handle = None
        return cls(reader, writer)

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await self.close()

    def __enter__(self):
        return thread.AWAIT(self.__aenter__())

    def __exit__(self, *args):
        return thread.AWAIT(self.__aexit__(*args))

    async def close(self):
        await self._reader.close()
        if self._reader != self._writer:
            await self._writer.close()

    async def send_bytes(self, buf, offset=0, size=None):
        '''
        Send a buffer of bytes as a single message
        '''
        m = memoryview(buf)
        if m.itemsize > 1:
            m = memoryview(bytes(m))
        n = len(m)
        if offset < 0:
            raise ValueError("offset is negative")
        if n < offset:
            raise ValueError("buffer length < offset")
        if size is None:
            size = n - offset
        elif size < 0:
            raise ValueError("size is negative")
        elif offset + size > n:
            raise ValueError("buffer length < offset + size")

        header = struct.pack('!i', size)
        if size >= 16384:
            await self._writer.write(header)
            await self._writer.write(m[offset:offset + size])
        else:
            msg = header + bytes(m[offset:offset + size])
            await self._writer.write(msg)
        return size

    async def recv_bytes(self, maxlength=None):
        '''
        Receive a message of bytes as a single message.
        '''
        header = await self._reader.read_exactly(4)
        size, = struct.unpack('!i', header)
        if maxlength and size > maxlength:
            raise IOError("Message too large")
        msg = await self._reader.read_exactly(size)
        return msg

    async def recv_bytes_into(self, buf, offset=0):
        '''
        Receive bytes into a writable memory buffer.  The buffer must be large enough to
        hold the message.  The number of bytes received in the message is returned.
        '''
        header = await self._reader.read_exactly(4)
        size, = struct.unpack('!i', header)
        with memoryview(buf).cast('B') as m:
            if size > (len(m) - offset):
                # Message is too large to fit in allotted space
                # Drain the I/O and raise an error
                while size > 0:
                    data = await self._reader.read(size)
                    if not data:
                        break
                    size -= len(data)
                raise IOError('Message is too large to fit')
            nread = await self._reader.readinto(m[offset:offset+size])
            if nread != size:
                raise EOFError('Expected end of data')
            return nread

    async def send(self, obj):
        '''
        Send an arbitrary Python object. Uses pickle to serialize.
        '''
        await self.send_bytes(pickle.dumps(obj, pickle.HIGHEST_PROTOCOL))

    async def recv(self):
        '''
        Receive a Python object. Uses pickle to unserialize.
        '''
        msg = await self.recv_bytes()
        return pickle.loads(msg)

    async def _deliver_challenge(self, authkey):
        message = os.urandom(AUTH_MESSAGE_LENGTH)
        await self.send_bytes(CHALLENGE + message)
        digest = hmac.new(authkey, message, 'md5').digest()
        response = await self.recv_bytes(maxlength=256)
        if response == digest:
            await self.send_bytes(WELCOME)
        else:
            await self.send_bytes(FAILURE)
            raise AuthenticationError('digest received was wrong')

    async def _answer_challenge(self, authkey):
        message = await self.recv_bytes(maxlength=256)
        assert message[:len(CHALLENGE)] == CHALLENGE, f'message = {message!r}'
        message = message[len(CHALLENGE):]
        digest = hmac.new(authkey, message, 'md5').digest()
        await self.send_bytes(digest)
        response = await self.recv_bytes(maxlength=256)

        if response != WELCOME:
            raise AuthenticationError('digest sent was rejected')

    async def authenticate_server(self, authkey):
        await self._deliver_challenge(authkey)
        await self._answer_challenge(authkey)

    async def authenticate_client(self, authkey):
        await self._answer_challenge(authkey)
        await self._deliver_challenge(authkey)

class Channel(object):
    def __init__(self, address, family=socket.AF_INET, check_address=None):
        self.address = address
        self.family = family
        self.sock = None
        if check_address:
            self.check_address = check_address

    def __repr__(self):
        return f'Channel({self.address!r}, {self.family!r})'

    async def __aenter__(self):
        return self

    async def __aexit__(self, ty, val, tb):
        await self.close()

    def __getstate__(self):
        return (self.address, self.family)

    def __setstate__(self, state):
        self.address, self.family = state
        self.sock = None

    def bind(self):
        self.sock = socket.socket(self.family, socket.SOCK_STREAM)
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, True)
        self.sock.bind(self.address)
        self.sock.listen(5)
        self.address = self.sock.getsockname()

    def check_address(self, addr):
        return True

    async def accept(self, *, authkey=None):
        if self.sock is None:
            self.bind()

        while True:
            client, addr = await self.sock.accept()
            if not self.check_address(addr):
                log.warning('Channel connection from %s rejected', addr)
                await client.close()
                del client
                continue

            client_stream = client.as_stream()
            c = Connection(client_stream, client_stream)
            c.address = addr
            try:
                async with timeout_after(1):
                    if authkey:
                        await c.authenticate_server(authkey)
                break
            except (TaskTimeout, AuthenticationError, EOFError):
                log.warning('Channel connection from %s failed', addr, exc_info=True)
                await c.close()
                del c
                del client_stream
                del client
        return c

    async def connect(self, *, authkey=None):
        sock = socket.socket(self.family, socket.SOCK_STREAM)
        await sock.connect(self.address)
        sock_stream = sock.as_stream()
        c = Connection(sock_stream, sock_stream)
        try:
            async with timeout_after(1):
                if authkey:
                    await c.authenticate_client(authkey)
            return c
        except TaskTimeout:
            log.warning('Channel connection to %s timed out', self.address)
            await c.close()
            del c
            del sock_stream
            # Note: Raising an OSError.
            raise TimeoutError("Connection timed out")

    async def close(self):
        if self.sock:
            await self.sock.close()
        self.sock = None
