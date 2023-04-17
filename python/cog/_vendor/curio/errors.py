# curio/errors.py
#
# Curio specific exceptions

__all__ = [
    'CurioError', 'CancelledError', 'TaskTimeout', 'TaskError',
    'SyncIOError', 'ResourceBusy',
    'ReadResourceBusy', 'WriteResourceBusy',
    'TimeoutCancellationError', 'UncaughtTimeoutError',
    'TaskCancelled', 'AsyncOnlyError',
]


class CurioError(Exception):
    '''
    Base class for all non-cancellation Curio-related exceptions
    '''


class CancelledError(BaseException):
    '''
    Base class for all task-cancellation related exceptions
    '''


class TaskCancelled(CancelledError):
    '''
    Exception raised as a result of a task being directly cancelled.
    '''


class TimeoutCancellationError(CancelledError):
    '''
    Exception raised if task is being cancelled due to a timeout, but
    it's not the inner-most timeout in effect.
    '''


class TaskTimeout(CancelledError):
    '''
    Exception raised if task is cancelled due to timeout.
    '''


class UncaughtTimeoutError(CurioError):
    '''
    Raised if a TaskTimeout exception escapes a timeout handling
    block and is unexpectedly caught by an outer timeout handler.
    '''


class TaskError(CurioError):
    '''
    Raised if a task launched via spawn() or similar function
    terminated due to an exception.  This is a chained exception.
    The __cause__ attribute contains the actual exception that
    occurred in the task.
    '''


class SyncIOError(CurioError):
    '''
    Raised if a task attempts to perform a synchronous I/O operation
    on an object that only supports asynchronous I/O.
    '''


class AsyncOnlyError(CurioError):
    '''
    Raised by the AWAIT() function if its applied to code not
    properly running in an async-thread.
    '''


class ResourceBusy(CurioError):
    '''
    Raised by I/O related functions if an operation is requested,
    but the resource is already busy performing the same operation
    on behalf of another task.
    '''


class ReadResourceBusy(ResourceBusy):
    pass


class WriteResourceBusy(ResourceBusy):
    pass


