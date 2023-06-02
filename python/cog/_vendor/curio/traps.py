# traps.py
#
# Curio programs execute under the supervision of a
# kernel. Communication with the kernel takes place via a "trap"
# involving the yield statement.  Traps represent internel kernel
# procedures.  Direct use of the functions defined here is allowed
# when making new kinds of Curio primitives, but if you're trying to
# solve a higher level problem, there is probably a higher-level
# interface that is easier to use (e.g., Socket, File, Queue, etc.).
# ----------------------------------------------------------------------

__all__ = [
    '_read_wait', '_write_wait', '_future_wait', '_sleep', '_spawn',
    '_cancel_task', '_scheduler_wait', '_scheduler_wake',
    '_get_kernel', '_get_current', '_set_timeout', '_unset_timeout',
    '_clock', '_io_waiting', '_io_release',
    ]

# -- Standard library

from types import coroutine
from selectors import EVENT_READ, EVENT_WRITE

# -- Curio

from . import errors

# This is the only entry point to the Curio kernel and the
# only place where the @types.coroutine decorator is used.
@coroutine
def _kernel_trap(*request):
    result = yield request
    if isinstance(result, BaseException):
        raise result
    else:
        return result

# Higher-level trap functions that make use of async/await
async def _read_wait(fileobj):
    '''
    Wait until reading can be performed.  If another task is waiting
    on the same file, a ResourceBusy exception is raised.
    '''
    return await _kernel_trap('trap_io', fileobj, EVENT_READ, 'READ_WAIT')

async def _write_wait(fileobj):
    '''
    Wait until writing can be performed. If another task is waiting
    to write on the same file, a ResourceBusy exception is raised.
    '''
    return await _kernel_trap('trap_io', fileobj, EVENT_WRITE, 'WRITE_WAIT')

async def _io_release(fileobj):
    '''
    Release kernel resources associated with a file
    '''
    return await _kernel_trap('trap_io_release', fileobj)

async def _io_waiting(fileobj):
    '''
    Return a tuple (rtask, wtask) of tasks currently blocked waiting
    for I/O on fileobj.
    '''
    return await _kernel_trap('trap_io_waiting', fileobj)

async def _future_wait(future, event=None):
    '''
    Wait for the result of a Future to be ready.
    '''
    return await _kernel_trap('trap_future_wait', future, event)

async def _sleep(clock):
    '''
    Sleep until the monotonic clock reaches the specified clock value.
    If clock is 0, forces the current task to yield to the next task (if any).
    '''
    return await _kernel_trap('trap_sleep', clock)

async def _spawn(coro):
    '''
    Create a new task. Returns the resulting Task object.
    '''
    return await _kernel_trap('trap_spawn', coro)

async def _cancel_task(task, exc=errors.TaskCancelled, val=None):
    '''
    Cancel a task. Causes a CancelledError exception to raise in the task.
    Set the exc and val arguments to change the exception.
    '''
    return await _kernel_trap('trap_cancel_task', task, exc, val)

async def _scheduler_wait(sched, state):
    '''
    Put the task to sleep on a scheduler primitive.
    '''
    return await _kernel_trap('trap_sched_wait', sched, state)

async def _scheduler_wake(sched, n=1):
    '''
    Reschedule one or more tasks waiting on a scheduler primitive.
    '''
    return await _kernel_trap('trap_sched_wake', sched, n)

async def _get_kernel():
    '''
    Get the kernel executing the task.
    '''
    return await _kernel_trap('trap_get_kernel')

async def _get_current():
    '''
    Get the currently executing task
    '''
    return await _kernel_trap('trap_get_current')

async def _set_timeout(clock):
    '''
    Set a timeout for the current task that occurs at the specified clock value.
    Setting a clock of None clears any previous timeout.
    '''
    return await _kernel_trap('trap_set_timeout', clock)

async def _unset_timeout(previous):
    '''
    Restore the previous timeout for the current task.
    '''
    return await _kernel_trap('trap_unset_timeout', previous)

async def _clock():
    '''
    Return the value of the kernel clock
    '''
    return await _kernel_trap('trap_clock')
