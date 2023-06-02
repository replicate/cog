# curio/thread.py
#
# Support for threads implemented on top of the Curio kernel.
#
# Theory of operation:
# --------------------
# Curio has the ability to safely wait for Futures as defined
# in the concurrent.futures module.  A notable feature of coroutines
# is that when called, their evaluation is delayed--instead you get
# a "coroutine" object that must be executed by a kernel or event loop.
#
# A so-called "async thread" uses both of these features together to
# set up an execution pathway for allowing threads to execute
# coroutines.  For each thread (a real thread--created by the
# threading module), a backing coroutine is created in Curio.  This
# backing coroutine runs on top of the Curio kernel and constantly
# monitors a Future for an incoming request.  This request is expected
# to contain an unevaluated coroutine. The unevaluated coroutine is
# evaluated on behalf of the thread by the backing coroutine.  Any
# result is the communicated back to the thread which is waiting
# for it on an Event.
#
# The mechanism for making a request within a thread is the AWAIT
# function.  Specifically, a call like this:
#
#       result = AWAIT(coro, *args, **kwargs)
#
# Makes the thread's backing coroutine execute the following:
#
#       result = await coro(*args, **kwargs)
#
# From the standpoint of the thread, it appears to be executing a
# normal synchronous call.
#
# Here is a picture diagram of the parts
#
#    ________          ___________          _________
#   |        | await  |           | Future |         |
#   | Curio  |<-------| backing   |<-------| Thread  |
#   | Kernel |------->| coroutine |------->|         |
#   |________| result |___________| Event  |_________|
#

__all__ = [ 'AWAIT', 'spawn_thread' ]

# -- Standard Library

import threading
from concurrent.futures import Future
from functools import wraps
from inspect import iscoroutine, isgenerator
from contextlib import contextmanager
import logging

log = logging.getLogger(__name__)

# -- Curio

from . import sync
from . import queue
from .task import spawn, disable_cancellation, check_cancellation, set_cancellation
from .traps import _future_wait
from . import errors
from . import meta

_locals = threading.local()

class AsyncThread(object):

    def __init__(self, target=None, args=(), kwargs={}, daemon=False):
        self.target = target
        self.args = args
        self.kwargs = kwargs
        self.daemon = daemon

        # The following attributes are provided to make a thread mimic a Task
        self.terminated = False
        self.cancelled = False
        self.taskgroup = None
        self.joined = False

        # This future is used by a thread to make a request to Curio
        self._request = Future()

        # This event is used to communicate completion of the request
        self._done_evt = threading.Event()

        # Event used to signal thread termination
        self._terminate_evt = sync.UniversalEvent()

        # Information about the coroutine being executed by the thread
        self._coro = None
        self._coro_result = None
        self._coro_exc = None

        # Final values produced by the thread before termination
        self._final_value = None
        self._final_exc = None

        # A reference to the associated thread (from threading module)
        self._thread = None

        # A reference to the associated backing task
        self._task = None

    async def _coro_runner(self):
        while True:
            # Wait for a hand-off
            await disable_cancellation(_future_wait(self._request))
            self._coro = self._request.result()
            self._request = Future()

            # If no coroutine, we're shutting down
            if not self._coro:
                break

            # Run the the coroutine
            try:
                self._coro_result = await self._coro
                self._coro_exc = None
            except BaseException as e:
                self._coro_result = None
                self._coro_exc = e

            # Hand it back to the thread
            self._coro = None
            self._done_evt.set()

        if self.taskgroup:
            await self.taskgroup._task_done(self)
            self.joined = True
        await self._terminate_evt.set()

    def _func_runner(self):
        _locals.thread = self
        try:
            self._final_result = self.target(*self.args, **self.kwargs)
            self._final_exc = None
        except BaseException as e:
            self._final_result = None
            self._final_exc = e
            if not isinstance(e, errors.CancelledError):
                log.warning("Unexpected exception in cancelled async thread", exc_info=True)

        finally:
            self._request.set_result(None)

    async def start(self):
        if self.target is None:
            raise RuntimeError("Async thread must be given a target")

        # Launch the backing coroutine
        self._task = await spawn(self._coro_runner, daemon=True)

        # Launch the thread itself
        self._thread = threading.Thread(target=self._func_runner)
        self._thread.start()

    def AWAIT(self, coro):
        self._request.set_result(coro)
        self._done_evt.wait()
        self._done_evt.clear()

        if self._coro_exc:
            raise self._coro_exc
        else:
            return self._coro_result

    async def join(self):
        await self.wait()
        self.joined = True
        if self.taskgroup:
            self.taskgroup._task_discard(self)

        if self._final_exc:
            raise errors.TaskError() from self._final_exc
        else:
            return self._final_result

    async def wait(self):
        await self._terminate_evt.wait()
        self.terminated = True

    @property
    def result(self):
        if not self._terminate_evt.is_set():
            raise RuntimeError('Thread not terminated')
        if self._final_exc:
            raise self._final_exc
        else:
            return self._final_result

    @property
    def exception(self):
        if not self._terminate_evt.is_set():
            raise RuntimeError('Thread not terminated')
        return self._final_exc

    async def cancel(self, *, exc=errors.TaskCancelled, blocking=True):
        self.cancelled = True
        await self._task.cancel(exc=exc, blocking=blocking)
        if blocking:
            await self.wait()

    @property
    def id(self):
        return self._task.id

    @property
    def state(self):
        return self._task.state

def AWAIT(coro, *args, **kwargs):
    '''
    Await for a coroutine in an asynchronous thread.  If coro is
    not a proper coroutine, this function acts a no-op, returning coro.
    '''
    # If the coro is a callable and it's identifiable as a coroutine function,
    # wrap it inside a coroutine and pass that.
    if callable(coro):
        if meta.iscoroutinefunction(coro) and hasattr(_locals, 'thread'):
            async def _coro(coro):
                return await coro(*args, **kwargs)
            coro = _coro(coro)
        else:
            coro = coro(*args, **kwargs)

    if iscoroutine(coro) or isgenerator(coro):
        if hasattr(_locals, 'thread'):
            return _locals.thread.AWAIT(coro)
        else:
            # Thought: Do we try to promote the calling thread into an
            # "async" thread automatically?  Would require a running
            # kernel. Would require a task dedicated to spawning the
            # coro runner.  Would require shutdown.  Maybe a context
            # manager?
            raise errors.AsyncOnlyError('Must be used as async')
    else:
        return coro

def spawn_thread(func, *args, daemon=False):
    '''
    Launch an async thread.  This mimicks the way a task is normally spawned. For
    example:

         t = await spawn_thread(func, arg1, arg2)
         ...
         await t.join()
    '''
    if iscoroutine(func) or meta.iscoroutinefunction(func):
          raise TypeError("spawn_thread() can't be used on coroutines")

    async def runner(args, daemon):
        t = AsyncThread(func, args=args, daemon=daemon)
        await t.start()
        return t

    return runner(args, daemon)

def is_async_thread():
    '''
    Returns True if current thread is an async thread.
    '''
    return hasattr(_locals, 'thread')
