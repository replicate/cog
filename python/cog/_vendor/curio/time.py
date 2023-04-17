# curio/time.py
#
# Functionality related to time handling including timeouts and sleeping

__all__ = [
    'clock', 'sleep', 'timeout_after', 'ignore_after',
    ]

# -- Standard library

import logging
log = logging.getLogger(__name__)

# --- Curio

from .task import current_task
from .traps import *
from .errors import *
from . import meta

async def clock():
    '''
    Immediately return the current value of the kernel clock. There
    are no side-effects such as task preemption or cancellation.
    '''
    return await _clock()

async def sleep(seconds):
    '''
    Sleep for a specified number of seconds.  Sleeping for 0 seconds
    makes a task immediately switch to the next ready task (if any).
    Returns the value of the kernel clock when awakened.
    '''
    return await _sleep(seconds)

class _TimeoutAfter(object):
    '''
    Helper class used by timeout_after() and ignore_after() functions
    when used as a context manager.  For example:

        async with timeout_after(delay):
            statements
            ...
    '''

    def __init__(self, clock, ignore=False, timeout_result=None):
        self._clock = clock
        self._ignore = ignore
        self._timeout_result = timeout_result
        self.expired = False
        self.result = True

    async def __aenter__(self):
        task = await current_task()
        # Clock adjusted to absolute time
        if self._clock is not None:
            self._clock += await _clock()
        self._deadlines = task._deadlines
        self._deadlines.append(self._clock)
        self._prior = await _set_timeout(self._clock)
        return self

    async def __aexit__(self, ty, val, tb):
        current_clock = await _unset_timeout(self._prior)

        # Discussion.  If a timeout has occurred, it will either
        # present itself here as a TaskTimeout or TimeoutCancellationError
        # exception.  The value of this exception is set to the current
        # kernel clock which can be compared against our own deadline.
        # What happens next is driven by these rules:
        #
        # 1.  If we are the outer-most context where the timeout
        #     period has expired, then a TaskTimeout is raised.
        #
        # 2.  If the deadline has expired for at least one outer
        #     context, (but not us), a TimeoutCancellationError is
        #     raised.  This means that time has expired elsewhere.
        #     We're being cancelled because of that, but the reason
        #     for the cancellation wasn't due to a timeout on our
        #     part.
        #
        # 3.  If the timeout period has not expired on ANY remaining
        #     timeout context, it means that a timeout has escaped
        #     some inner timeout context where it should have been
        #     caught. This is an operational error.  We raise
        #     UncaughtTimeoutError.

        try:
            if ty in (TaskTimeout, TimeoutCancellationError):
                timeout_clock = val.args[0]
                # Find the outer most deadline that has expired
                for n, deadline in enumerate(self._deadlines):
                    if deadline <= timeout_clock:
                        break
                else:
                    # No remaining context has expired. An operational error
                    raise UncaughtTimeoutError('Uncaught timeout received')

                if n < len(self._deadlines) - 1:
                    if ty is TaskTimeout:
                        raise TimeoutCancellationError(val.args[0]).with_traceback(tb) from None
                    else:
                        return False
                else:
                    # The timeout is us.  Make sure it's a TaskTimeout (unless ignored)
                    self.result = self._timeout_result
                    self.expired = True
                    if self._ignore:
                        return True
                    else:
                        if ty is TimeoutCancellationError:
                            raise TaskTimeout(val.args[0]).with_traceback(tb) from None
                        else:
                            return False
            elif ty is None:
                if current_clock > self._deadlines[-1]:
                    # Further discussion.  In the presence of threads and blocking
                    # operations, it's possible that a timeout has expired, but
                    # there was simply no opportunity to catch it because there was
                    # no suspension point.
                    badness = current_clock - self._deadlines[-1]
                    log.warning('%r. Operation completed successfully, '
                                'but it took longer than an enclosing timeout. Badness delta=%r.',
                                await current_task(), badness)

        finally:
            self._deadlines.pop()

    def __enter__(self):
        return thread.AWAIT(self.__aenter__())

    def __exit__(self, *args):
        return thread.AWAIT(self.__aexit__(*args))

async def _timeout_after_func(clock, coro, args,
                              ignore=False, timeout_result=None):
    coro = meta.instantiate_coroutine(coro, *args)
    async with _TimeoutAfter(clock, ignore=ignore, timeout_result=timeout_result):
        return await coro

def timeout_after(seconds, coro=None, *args):
    '''
    Raise a TaskTimeout exception in the calling task after seconds
    have elapsed.  This function may be used in two ways. You can
    apply it to the execution of a single coroutine:

         await timeout_after(seconds, coro(args))

    or you can use it as an asynchronous context manager to apply
    a timeout to a block of statements:

         async with timeout_after(seconds):
             await coro1(args)
             await coro2(args)
             ...
    '''
    if coro is None:
        return _TimeoutAfter(seconds)
    else:
        return _timeout_after_func(seconds, coro, args)

def ignore_after(seconds, coro=None, *args, timeout_result=None):
    '''
    Stop the enclosed task or block of code after seconds have
    elapsed.  No exception is raised when time expires. Instead, None
    is returned.  This is often more convenient that catching an
    exception.  You can apply the function to a single coroutine:

        if await ignore_after(5, coro(args)) is None:
            # A timeout occurred
            ...

    Alternatively, you can use this function as an async context
    manager on a block of statements like this:

        async with ignore_after(5) as r:
            await coro1(args)
            await coro2(args)
            ...
        if r.result is None:
            # A timeout occurred

    When used as a context manager, the return manager object has
    a result attribute that will be set to None if the time
    period expires (or True otherwise).

    You can change the return result to a different value using
    the timeout_result keyword argument.
    '''
    if coro is None:
        return _TimeoutAfter(seconds, ignore=True, timeout_result=timeout_result)
    else:
        return _timeout_after_func(seconds, coro, args, ignore=True, timeout_result=timeout_result)

from . import thread
