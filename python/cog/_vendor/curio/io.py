# curio/io.py
#
# Curio is primarily concerned with the scheduling of tasks. In
# particular, the kernel does not actually perform any I/O.  It merely
# blocks tasks that need to wait for reading or writing.  To actually
# perform I/O, you use the existing file and socket abstractions
# already provided by the Python standard library.  The only
# difference is that you need to take extra steps to manage their
# non-blocking behavior.  The classes in this file provide wrappers
# around socket-like and file-like objects. Methods responsible for
# reading/writing have a small amount of extra logic to added to
# handle their scheduling.  Other methods are simply passed through to
# the original object via delegation.
#
# It's important to emphasize that these classes can be applied to
# *ANY* existing socket-like or file-like object as long as it
# represents a real system-level file (must have a fileno() method)
# and can be used with the underlying I/O selector.  For example, the
# Socket class can wrap a normal socket or an SSL socket--it doesn't
# matter which.  Similarly, the Stream class can wrap normal files,
# files created from sockets, pipes, and other file-like abstractions.
#
# No assumption is made about system compatibility (Unix vs. Windows).
# The main compatibility concern would be at the level of the I/O
# selector used by the kernel.  For example, can it detect I/O events
# on the provided file or socket?  If so, it will probably work here.

__all__ = ['Socket', 'FileStream', 'SocketStream']

# -- Standard Library

from socket import SOL_SOCKET, SO_ERROR
from contextlib import contextmanager
import io
import os

# -- Curio

from .traps import _read_wait, _write_wait, _io_release
from . import errors
from . import thread

# Exceptions raised for non-blocking I/O.  For normal sockets, blocking operations
# normally just raise BlockingIOError.  For SSL sockets, more specific exceptions
# are raised.  Here we're just making some aliases for the possible exceptions.

try:
    from ssl import SSLWantReadError, SSLWantWriteError
    WantRead = (BlockingIOError, InterruptedError, SSLWantReadError)
    WantWrite = (BlockingIOError, InterruptedError, SSLWantWriteError)
except ImportError:
    WantRead = (BlockingIOError, InterruptedError)
    WantWrite = (BlockingIOError, InterruptedError)

# Wrapper class around an integer file descriptor. This is used
# to take advantage of an I/O scheduling performance optimization
# in the kernel.  If a non-integer file object is given, the
# kernel is able to reuse prior registrations on the event loop.
# The reason this wrapper class is used is that even though an
# integer file descriptor might be reused by the host OS,
# instances of _Fd will not be reused. Thus, if a file is closed
# and a new file opened on the same descriptor, it will be
# detected as a different file.
#
# See also: https://github.com/dabeaz/curio/issues/104


class _Fd(object):
    __slots__ = ('fd',)

    def __init__(self, fd):
        self.fd = fd

    def fileno(self):
        return self.fd

    def __int__(self):
        return self.fd

    def __repr__(self):
        return f'<fd={self.fd!r}>'


# There is a certain amount of repetition in this class.  It can
# probably be shortened with some sort of decorator magic. On the
# other, the KISSS (Keep it Stupid Simple Stupid) principle might be a
# better policy--just in case someone needs to debug it.

class Socket(object):
    '''
    Non-blocking wrapper around a socket object.   The original socket is put
    into a non-blocking mode when it's wrapped.
    '''

    def __init__(self, sock):
        self._socket = sock
        self._socket.setblocking(False)
        self._fileno = _Fd(sock.fileno())

        # Commonly used bound methods
        self._socket_send = sock.send
        self._socket_recv = sock.recv

    def __repr__(self):
        return f'<curio.Socket {self._socket!r}>'

    def __getattr__(self, name):
        return getattr(self._socket, name)

    def fileno(self):
        return int(self._fileno)

    def settimeout(self, seconds):
        raise RuntimeError('Use timeout_after() to set a timeout')

    def gettimeout(self):
        return None

    def dup(self):
        return type(self)(self._socket.dup())

    def makefile(self, mode, buffering=0, *, encoding=None, errors=None, newline=None):
        if 'b' not in mode:
            raise RuntimeError('File can only be created in binary mode')
        f = self._socket.makefile(mode, buffering=buffering)
        return FileStream(f)

    def as_stream(self):
        '''
        Create a stream-based interface to the socket.
        '''
        return SocketStream(self._socket)

    @contextmanager
    def blocking(self):
        '''
        Allow temporary access to the underlying socket in blocking mode
        '''
        try:
            self._socket.setblocking(True)
            yield self._socket
        finally:
            self._socket.setblocking(False)

    async def recv(self, maxsize, flags=0):
        while True:
            try:
                return self._socket_recv(maxsize, flags)
            except WantRead:
                await _read_wait(self._fileno)
            except WantWrite:     # pragma: no cover
                await _write_wait(self._fileno)

    async def recv_into(self, buffer, nbytes=0, flags=0):
        while True:
            try:
                return self._socket.recv_into(buffer, nbytes, flags)
            except WantRead:
                await _read_wait(self._fileno)
            except WantWrite:     # pragma: no cover
                await _write_wait(self._fileno)

    async def send(self, data, flags=0):
        while True:
            try:
                return self._socket_send(data, flags)
            except WantWrite:
                await _write_wait(self._fileno)
            except WantRead:      # pragma: no cover
                await _read_wait(self._fileno)

    async def sendall(self, data, flags=0):
        with memoryview(data).cast('B') as buffer:
            total_sent = 0
            try:
                while buffer:
                    try:
                        nsent = self._socket_send(buffer, flags)
                        total_sent += nsent
                        buffer = buffer[nsent:]
                    except WantWrite:
                        await _write_wait(self._fileno)
                    except WantRead:   # pragma: no cover
                        await _read_wait(self._fileno)
            except errors.CancelledError as e:
                e.bytes_sent = total_sent
                raise

    async def accept(self):
        while True:
            try:
                client, addr = self._socket.accept()
                return type(self)(client), addr
            except WantRead:
                await _read_wait(self._fileno)

    async def connect_ex(self, address):
        try:
            await self.connect(address)
            return 0
        except OSError as e:
            return e.errno

    async def connect(self, address):
        try:
            result = self._socket.connect(address)
            if getattr(self, 'do_handshake_on_connect', False):
                await self.do_handshake()
            return result
        except WantWrite:
            await _write_wait(self._fileno)
        err = self._socket.getsockopt(SOL_SOCKET, SO_ERROR)
        if err != 0:
            raise OSError(err, f'Connect call failed {address}')
        if getattr(self, 'do_handshake_on_connect', False):
            await self.do_handshake()

    async def recvfrom(self, buffersize, flags=0):
        while True:
            try:
                return self._socket.recvfrom(buffersize, flags)
            except WantRead:
                await _read_wait(self._fileno)
            except WantWrite:
                await _write_wait(self._fileno)

    async def recvfrom_into(self, buffer, bytes=0, flags=0):
        while True:
            try:
                return self._socket.recvfrom_into(buffer, bytes, flags)
            except WantRead:
                await _read_wait(self._fileno)
            except WantWrite:       # pragma: no cover
                await _write_wait(self._fileno)

    async def sendto(self, bytes, flags_or_address, address=None):
        if address:
            flags = flags_or_address
        else:
            address = flags_or_address
            flags = 0
        while True:
            try:
                return self._socket.sendto(bytes, flags, address)
            except WantWrite:
                await _write_wait(self._fileno)
            except WantRead:      # pragma: no cover
                await _read_wait(self._fileno)

    async def recvmsg(self, bufsize, ancbufsize=0, flags=0):
        while True:
            try:
                return self._socket.recvmsg(bufsize, ancbufsize, flags)
            except WantRead:
                await _read_wait(self._fileno)

    async def recvmsg_into(self, buffers, ancbufsize=0, flags=0):
        while True:
            try:
                return self._socket.recvmsg_into(buffers, ancbufsize, flags)
            except WantRead:
                await _read_wait(self._fileno)

    async def sendmsg(self, buffers, ancdata=(), flags=0, address=None):
        while True:
            try:
                return self._socket.sendmsg(buffers, ancdata, flags, address)
            except WantRead:
                await _write_wait(self._fileno)

    # Special functions for SSL
    async def do_handshake(self):
        while True:
            try:
                return self._socket.do_handshake()
            except WantRead:
                await _read_wait(self._fileno)
            except WantWrite:
                await _write_wait(self._fileno)

    # Design discussion.  Why make close() async?   Partly it's to make the
    # programming interface highly uniform with the other methods (all of which
    # involve an await).  It's also to provide consistency with the Stream
    # API below which requires an asynchronous close to properly flush I/O
    # buffers.

    async def close(self):
        if self._socket:
            await _io_release(self._fileno)
            self._socket.close()
        self._socket = None
        self._fileno = -1

    # This is declared as async for the same reason as close()
    async def shutdown(self, how):
        if self._socket:
            self._socket.shutdown(how)

    async def __aenter__(self):
        self._socket.__enter__()
        return self

    async def __aexit__(self, *args):
        if self._socket:
            self._socket.__exit__(*args)

    def __enter__(self):
        return thread.AWAIT(self.__aenter__())

    def __exit__(self, *args):
        return thread.AWAIT(self.__aexit__(*args))


MAX_READ = 65536


class StreamBase(object):
    '''
    Base class for file-like objects.
    '''

    def __init__(self, fileobj):
        self._file = fileobj
        self._fileno = _Fd(fileobj.fileno())
        self._buffer = bytearray()

    def __repr__(self):
        return f'<curio.{type(self).__name__} {self._file!r}>'

    def fileno(self):
        return int(self._fileno)

    # ---- Methods that must be implemented in child classes
    async def _read(self, maxbytes=-1):     # pragma: no cover
        raise NotImplementedError()

    async def write(self, data):            # pragma: no cover
        raise NotImplementedError()

    # ---- General I/O methods for streams
    async def read(self, maxbytes=-1):
        buf = self._buffer
        if buf:
            if maxbytes < 0 or len(buf) <= maxbytes:
                data = bytes(buf)
                buf.clear()
            else:
                data = bytes(buf[:maxbytes])
                del buf[:maxbytes]
        else:
            data = await self._read(maxbytes)
        return data

    async def readall(self):
        chunks = []
        maxread = 65536
        if self._buffer:
            chunks.append(bytes(self._buffer))
            self._buffer.clear()
        while True:
            try:
                chunk = await self.read(maxread)
            except errors.CancelledError as e:
                e.bytes_read = b''.join(chunks)
                raise
            if not chunk:
                return b''.join(chunks)
            chunks.append(chunk)
            if len(chunk) == maxread:
                maxread *= 2

    async def read_exactly(self, nbytes):
        chunks = []
        while nbytes > 0:
            try:
                chunk = await self.read(nbytes)
            except errors.CancelledError as e:
                e.bytes_read = b''.join(chunks)
                raise
            if not chunk:
                e = EOFError('Unexpected end of data')
                e.bytes_read = b''.join(chunks)
                raise e
            chunks.append(chunk)
            nbytes -= len(chunk)
        return b''.join(chunks)

    async def readinto(self, memory):
        with memoryview(memory).cast('B') as view:
            remaining = len(view)
            total_read = 0

            # It's possible that there is data already buffered on this stream.
            # If so, we have to copy into the memory buffer first.
            buffered = len(self._buffer)
            tocopy = remaining if (remaining < buffered) else buffered
            if tocopy:
                view[:tocopy] = self._buffer[:tocopy]
                del self._buffer[:tocopy]
                remaining -= tocopy
                total_read += tocopy

            # To emulate behavior of synchronous readinto(), we read all available
            # bytes up to the buffer size.
            while remaining > 0:
                try:
                    nrecv = self._readinto_impl(view[total_read:total_read+remaining])

                    # On proper file objects, None might be returned to indicate blocking
                    if nrecv is None:
                        await _read_wait(self._fileno)
                    elif nrecv == 0:
                        break
                    else:
                        total_read += nrecv
                        remaining -= nrecv
                except WantRead:
                    await _read_wait(self._fileno)
                except WantWrite:
                    await _write_wait(self._fileno)
            return total_read

    async def readline(self, maxlen=None):
        while True:
            nl_index = self._buffer.find(b'\n')
            if nl_index >= 0:
                resp = bytes(self._buffer[:nl_index + 1])
                del self._buffer[:nl_index + 1]
                return resp
            data = await self._read(MAX_READ)
            if data == b'':
                resp = bytes(self._buffer)
                self._buffer.clear()
                return resp
            self._buffer.extend(data)

    async def readlines(self):
        lines = []
        try:
            async for line in self:
                lines.append(line)
            return lines
        except errors.CancelledError as e:
            e.lines_read = lines
            raise

    async def writelines(self, lines):
        nwritten = 0
        for line in lines:
            try:
                await self.write(line)
                nwritten += len(line)
            except errors.CancelledError as e:
                e.bytes_written += nwritten
                raise

    async def flush(self):
        pass

    # Why async close()?   If the underlying file is buffered, the contents need
    # to be flushed first--a process that might cause a BlockingIOError.  In
    # that case, we have to suspend briefly until the buffers free up space.
    async def close(self):
        await self.flush()
        if self._file:
            await _io_release(self.fileno)
            self._file.close()
        self._file = None
        self._fileno = -1

    def __aiter__(self):
        return self

    async def __anext__(self):
        line = await self.readline()
        if line:
            return line
        else:
            raise StopAsyncIteration

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await self.close()

    def __iter__(self):
        return thread.AWAIT(self.__aiter__())

    def __next__(self):
        try:
            return thread.AWAIT(self.__anext__())
        except StopAsyncIteration:
            raise StopIteration

    def __enter__(self):
        return thread.AWAIT(self.__aenter__())

    def __exit__(self, *args):
        return thread.AWAIT(self.__exit__(*args))


class FileStream(StreamBase):
    '''
    Wrapper around a file-like object.  File is put into non-blocking mode.
    The underlying file must be in binary mode.
    '''

    def __init__(self, fileobj):
        assert not isinstance(fileobj, io.TextIOBase), 'Only binary mode files allowed'
        super().__init__(fileobj)
        os.set_blocking(int(self._fileno), False)

        # Common bound methods
        self._file_read = fileobj.read
        self._readinto_impl = getattr(fileobj, 'readinto', None)
        self._file_write = fileobj.write

    @contextmanager
    def blocking(self):
        '''
        Allow temporary access to the underlying file in blocking mode
        '''
        if self._buffer:
            raise IOError('There is unread buffered data.')
        try:
            os.set_blocking(int(self._fileno), True)
            yield self._file
        finally:
            os.set_blocking(int(self._fileno), False)

    async def _read(self, maxbytes=-1):
        while True:
            # In non-blocking mode, a file-like object might return None if no data is
            # available.  Alternatively, we'll catch the usual blocking exceptions just to be safe
            try:
                data = self._file_read(maxbytes)
                if data is None:
                    await _read_wait(self._fileno)
                else:
                    return data
            except WantRead:
                await _read_wait(self._fileno)
            except WantWrite:  # pragma: no cover
                await _write_wait(self._fileno)

    async def write(self, data):
        nwritten = 0
        view = memoryview(data).cast('B')
        try:
            while view:
                try:
                    nbytes = self._file_write(view)
                    if nbytes is None:
                        raise BlockingIOError()
                    nwritten += nbytes
                    view = view[nbytes:]
                except WantWrite as e:
                    if hasattr(e, 'characters_written'):
                        nwritten += e.characters_written
                        view = view[e.characters_written:]
                    await _write_wait(self._fileno)
                except WantRead:   # pragma: no cover
                    await _read_wait(self._fileno)
            return nwritten

        except errors.CancelledError as e:
            e.bytes_written = nwritten
            raise

    async def flush(self):
        if not self._file:
            return
        while True:
            try:
                return self._file.flush()
            except WantWrite:
                await _write_wait(self._fileno)
            except WantRead:
                await _read_wait(self._fileno)


class SocketStream(StreamBase):
    '''
    Stream wrapper for a socket.
    '''

    def __init__(self, sock):
        super().__init__(sock)
        sock.setblocking(False)

        # Common bound methods
        self._socket_recv = sock.recv
        self._readinto_impl = sock.recv_into
        self._socket_send = sock.send

    @contextmanager
    def blocking(self):
        '''
        Allow temporary access to the underlying file in blocking mode
        '''
        if self._buffer:
            raise IOError('There is unread buffered data.')
        try:
            self._file.setblocking(True)
            yield open(int(self._fileno), 'rb+', buffering=0, closefd=False)
        finally:
            self._file.setblocking(False)

    async def _read(self, maxbytes=-1):
        while True:
            try:
                data = self._socket_recv(maxbytes if maxbytes > 0 else MAX_READ)
                return data
            except WantRead:
                await _read_wait(self._fileno)
            except WantWrite:        # pragma: no cover
                await _write_wait(self._fileno)

    async def write(self, data):
        nwritten = 0
        view = memoryview(data).cast('B')
        try:
            while view:
                try:
                    nbytes = self._socket_send(view)
                    nwritten += nbytes
                    view = view[nbytes:]
                except WantWrite:
                    await _write_wait(self._fileno)
                except WantRead:     # pragma: no cover
                    await _read_wait(self._fileno)
            return nwritten
        except errors.CancelledError as e:
            e.bytes_written = nwritten
            raise

