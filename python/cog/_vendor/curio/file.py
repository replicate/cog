# curio/file.py
#
# Let's talk about files for a moment.  Suppose you're in a coroutine
# and you start using things like the built-in open() function:
#
#     async def coro():
#         f = open(somefile, 'r')
#         data = f.read()
#         ...
#
# Yes, it will "work", but who knows what's actually going to happen
# on that open() call and associated read().  If it's on disk, the
# whole program might lock up for a few milliseconds (aka. "an
# eternity") doing a disk seek.  While that happens, your whole
# coroutine based server is going to grind to a screeching halt.  This
# is bad--especially if a lot of coroutines start doing it all at
# once.
#
# Knowing how to handle this is a tricky question.  Traditional files
# don't really support "async" in the usual way a socket might. You
# might be able to do something sneaky with asynchronous POSIX APIs
# (i.e., aio_* functions) or maybe thread pools.  However, one thing
# is for certain--if files are going to be handled in a sane way, they're
# going to have an async interface.
#
# This file does just that by providing an async-compatible aopen()
# call.  You use it the same way you use open() and a normal file:
#
#    async def coro():
#        async with aopen(somefile, 'r') as f:
#            data = await f.read()
#            ...
#
# If you want to use iteration, make sure you use the asynchronous version:
#
#    async def coro():
#        async with aopen(somefile, 'r') as f:
#            async for line in f:
#                ...
#

__all__ = ['aopen', 'anext']

# -- Standard library

from contextlib import contextmanager
from functools import partial

# -- Curio

from .workers import run_in_thread
from .errors import SyncIOError, CancelledError
from . import thread

class AsyncFile(object):
    '''
    An async wrapper around a standard file object.  Uses threads to
    execute various I/O operations in a way that avoids blocking
    the Curio kernel loop.
    '''

    def __init__(self, fileobj, open_args=None, open_kwargs=None):
        self._fileobj = fileobj
        self._open_args = open_args
        self._open_kwargs = open_kwargs

    def __repr__(self):
        return 'AsyncFile(%r)' % self._fileobj

    @contextmanager
    def blocking(self):
        '''
        Expose the underlying file in blocking mode for use with synchronous code.
        '''
        yield self._file

    @property
    def _file(self):
        if self._fileobj is None:
            raise RuntimeError('Must use an async file as an async-context-manager.')
        return self._fileobj

    async def read(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.read, *args, **kwargs))

    async def read1(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.read1, *args, **kwargs))

    async def readinto(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.readinto, *args, **kwargs))

    async def readinto1(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.readinto1, *args, **kwargs))

    async def readline(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.readline, *args, **kwargs))

    async def readlines(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.readlines, *args, **kwargs))

    async def write(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.write, *args, **kwargs))

    async def writelines(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.writelines, *args, **kwargs))

    async def flush(self):
        return await run_in_thread(self._file.flush)

    async def close(self):
        return await run_in_thread(self._file.close)

    async def seek(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.seek, *args, **kwargs))

    async def tell(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.tell, *args, **kwargs))

    async def truncate(self, *args, **kwargs):
        return await run_in_thread(partial(self._file.truncate, *args, **kwargs))

    def __iter__(self):
        raise SyncIOError('Use asynchronous iteration')

    def __next__(self):
        raise SyncIOError('Use asynchronous iteration')

    def __enter__(self):
        return thread.AWAIT(self.__aenter__())

    def __exit__(self, *args):
        return thread.AWAIT(self.__aexit__(*args))

    def __aiter__(self):
        return self

    async def __aenter__(self):
        if self._fileobj is None:
            self._fileobj = await run_in_thread(partial(open, *self._open_args, **self._open_kwargs))
        return self

    async def __aexit__(self, *args):
        await self.close()

    async def __anext__(self):
        data = await run_in_thread(next, self._file, None)
        if data is None:
            raise StopAsyncIteration
        return data

    def __getattr__(self, name):
        return getattr(self._file, name)

    # Compatibility with io.FileStream
    async def readall(self):
        chunks = []
        maxread = 65536
        sep = '' if hasattr(self._file, 'encoding') else b''
        while True:
            try:
                chunk = await self.read(maxread)
            except CancelledError as e:
                e.bytes_read = sep.join(chunks)
                raise
            if not chunk:
                return sep.join(chunks)
            chunks.append(chunk)
            if len(chunk) == maxread:
                maxread *= 2

def aopen(*args, **kwargs):
    '''
    Async version of the builtin open() function that returns an async-compatible
    file object.  Takes the same arguments.  Returns a wrapped file in which
    blocking I/O operations must be awaited.
    '''
    return AsyncFile(None, args, kwargs)

async def anext(f, sentinel=object):
    '''
    Async version of the builtin next() function that advances an async iterator.
    Sometimes used to skip a single line in files.
    '''
    try:
        return await f.__anext__()
    except StopAsyncIteration:
        if sentinel is not object:
            return sentinel
        else:
            raise
