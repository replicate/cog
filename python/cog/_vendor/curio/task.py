# curio/task.py
#
# Task class and task related functions such as spawning and cancellation.

__all__ = [
    'Task', 'TaskGroup', 'current_task', 'spawn', 
    'disable_cancellation', 'check_cancellation', 'set_cancellation'
]

# -- Standard library

import logging
from collections import deque
import linecache
import traceback
import os.path

log = logging.getLogger(__name__)

# -- Curio

from .errors import *
from .traps import *
from .sched import SchedBarrier
from . import meta

# Internal functions used for debugging/diagnostics
def _get_stack(coro):
    '''
    Extracts a list of stack frames from a chain of generator/coroutine calls
    '''
    frames = []
    while coro:
        if hasattr(coro, 'cr_frame'):
            f = coro.cr_frame
            coro = coro.cr_await
        elif hasattr(coro, 'ag_frame'):
            f = coro.ag_frame
            coro = coro.ag_await
        elif hasattr(coro, 'gi_frame'):
            f = coro.gi_frame
            coro = coro.gi_yieldfrom
        else:
            # Note: Can't proceed further.  Need the ags_gen or agt_gen attribute
            # from an asynchronous generator.  See https://bugs.python.org/issue32810
            f = None
            coro = None

        if f is not None:
            frames.append(f)
    return frames

# Create a stack traceback for a task
def _format_stack(task, complete=False):
    '''
    Formats a traceback from a stack of coroutines/generators
    '''
    dirname = os.path.dirname(__file__)
    extracted_list = []
    checked = set()
    for f in _get_stack(task.coro):
        lineno = f.f_lineno
        co = f.f_code
        filename = co.co_filename
        name = co.co_name
        if not complete and os.path.dirname(filename) == dirname:
            continue
        if filename not in checked:
            checked.add(filename)
            linecache.checkcache(filename)
        line = linecache.getline(filename, lineno, f.f_globals)
        extracted_list.append((filename, lineno, name, line))
    if not extracted_list:
        resp = f'No stack for {task!r}'
    else:
        resp = f'Stack for {task!r} (most recent call last):\n'
        resp += ''.join(traceback.format_list(extracted_list))
    return resp

# Return the (filename, lineno) where a task is currently executing
def _where(task):
    dirname = os.path.dirname(__file__)
    for f in _get_stack(task.coro):
        lineno = f.f_lineno
        co = f.f_code
        filename = co.co_filename
        name = co.co_name
        if os.path.dirname(filename) == dirname:
            continue
        return filename, lineno
    return None, None

class Task(object):
    '''
    The Task class wraps a coroutine and provides some additional attributes
    related to execution state and debugging.  Tasks are not normally 
    instantiated directly. Instead, use spawn().
    '''
    _lastid = 1
    def __init__(self, coro, parent=None):
        # Informational attributes about the task itself
        self.id = Task._lastid
        Task._lastid += 1
        self.parentid = None if parent is None else parent.id  # Parent task id (if any)
        self.coro = coro              # Underlying generator/coroutine
        self.name = getattr(coro, '__qualname__', str(coro))
        self.daemon = False           # Daemonic flag

        # Attributes updated during execution (safe to inspect)
        self.cycles = 0               # Execution cycles completed
        self.state = 'INITIAL'        # Execution state
        self.cancel_func = None       # Cancellation function
        self.future = None            # Pending Future (if any)
        self.sleep = None             # Pending sleep (if any)
        self.timeout = None           # Pending timeout (if any)
        self.joining = SchedBarrier() # Set of tasks waiting to join with this one
        self.cancelled = None         # Has the task been cancelled?
        self.terminated = False       # Has the task actually Terminated?
        self.cancel_pending = None    # Deferred cancellation exception pending (if any)
        self.allow_cancel = True      # Can cancellation exceptions be delivered?
        self.taskgroup = None         # Containing task group (if any)
        self.joined = False           # Set if the task has actually been joined or result collected

        # Final result of coroutine execution (use properties to access)
        self._final_result = None     # Final result of execution
        self._final_exc = None        # Final exception of execution

        # Actual execution is wrapped by a supporting coroutine
        self._run_coro = self._task_runner(self.coro)

        # Result of the last trap
        self._trap_result = None 

        # Last I/O operation performed
        self._last_io = None          

        # Bound coroutine methods
        self._send = self._run_coro.send   
        self._throw = self._run_coro.throw
        
        # Timeout deadline stack
        self._deadlines = []

    def __repr__(self):
        return f'{type(self).__name__}(id={self.id}, name={self.name!r}, state={self.state!r})'

    def __str__(self):
        filename, lineno = _where(self)
        if filename:
            return f'{self!r} at {filename}:{lineno}'
        else:
            return repr(self)

    def __del__(self):
        self.coro.close()
        if not self.joined and not self.cancelled and not self.daemon:
            if not self.daemon and not self.exception:
                log.warning('%r never joined', self)
        assert not self._last_io

    def send(self, value):
        '''
        Send the next value into the task coroutine.  This method is
        a wrapper around the actual method and may be subclassed to
        implement different kinds of low-level functionality.
        '''
        return self._send(value)

    async def _task_runner(self, coro):
        try:
            return await coro
        finally:
            if self.taskgroup:
                self.joined = True
                await self.taskgroup._task_done(self)

    async def join(self):
        '''
        Wait for a task to terminate.  Returns the return value (if any)
        or raises a TaskError if the task crashed with an exception.
        '''
        await self.wait()
        if self.taskgroup:
            self.taskgroup._task_discard(self)
        self.joined = True
        if self.exception:
            raise TaskError('Task crash') from self.exception
        else:
            return self.result

    async def wait(self):
        '''
        Wait for a task to terminate. Does not return any value.
        '''
        if not self.terminated:
            await self.joining.suspend('TASK_JOIN')
        
    @property
    def result(self):
        '''
        Return the result of a task. The task must be terminated already.
        '''
        if not self.terminated:
            raise RuntimeError('Task not terminated')
        self.joined = True
        if self._final_exc:
            raise self._final_exc
        else:
            return self._final_result

    @result.setter
    def result(self, value):
        self._final_result = value
        self._final_exc = None

    @property
    def exception(self):
        '''
        Return any pending exception of a task or None.
        '''
        if not self.terminated:
            raise RuntimeError('Task not terminated')
        return self._final_exc

    @exception.setter
    def exception(self, value):
        self._final_result = None
        self._final_exc = value

    async def cancel(self, *, exc=TaskCancelled, blocking=True):
        '''
        Cancel a task by raising a CancelledError exception.

        If blocking=False, schedules the cancellation and returns immediately.

        If blocking=True (the default), then does not
        return until the task actually terminates.
        '''
        if self.terminated:
            self.joined = True
            return
        await _cancel_task(self, exc=exc)
        if blocking:
            await self.wait()

    def traceback(self):
        '''
        Return a formatted traceback showing where the task is currently executing.
        '''
        return _format_stack(self)

    def where(self):
        '''
        Return a tuple (filename, lineno) where task is executing
        '''
        return _where(self)

class ContextTask(Task):
    '''
    Task class that provides support for contextvars.  Use with the
    taskcls keyword argument to the Curio kernel.
    '''
    def __init__(self, coro, parent=None):
        import contextvars
        super().__init__(coro)
        if parent:
            parent._context.run(lambda: setattr(self, '_context', contextvars.copy_context()))
        else:
            self._context = contextvars.copy_context()

    def send(self, value):
        return self._context.run(super().send, value)

class TaskGroup(object):
    '''
    A TaskGroup represents a collection of managed tasks.  A group can
    be used to ensure that all tasks terminate together, to monitor
    tasks as they finish, and to collect results.

    A TaskGroup can be created from existing tasks.  For example:

        t1 = await spawn(coro1)
        t2 = await spawn(coro2)
        t3 = await spawn(coro3)

        async with TaskGroup([t1,t2,t3]) as g:
            ...

    Alternatively, tasks can be spawned into a task group.

        async with TaskGroup(wait) as g:
            await g.spawn(coro1)
            await g.spawn(coro2)
            await g.spawn(coro3)

    When used as a context manager, a TaskGroup will wait until
    all contained tasks exit before moving on.   The optional wait argument
    specifies a strategy.  If wait=all (the default), a task group
    waits for all tasks to exit.  If wait=any, the group waits
    for the first task to exit. If wait=object, the group waits for 
    the first task to return a non-None result.  If wait=None, the
    group immediately cancels all running tasks.

    Task groups are often used to gather results. The following
    properties are useful:

        async with TaskGroup() as g:
            ...

        print(g.result)    # Result of the first task to exit
        print(g.results)   # List of all results computed

    Note: Both of these properties may raise an exception if a task
    exited in error. The g.tasks property is a list of all
    managed tasks listed in order of creation. 

    To obtain tasks in the order that they complete as they complete,
    use iteration:
  
        async with TaskGroup() as g:
            await g.spawn(coro1)
            await g.spawn(coro2)
            await g.spawn(coro3)

            async for done in g:
                print('Task done', done, done.result)

    The cancel_remaining() method can be used to cancel all
    remaining tasks early.  The add_task() method can be
    used to add an already existing task to a group.  Calling
    .join() on a specific task removes it from a group. For example:

         async with TaskGroup() as g:
             t1 = await g.spawn(coro1)
             ...
             await t1.join()        # removes t1 from the group

    Normally, a task group is used as a context manager.  This 
    doesn't have to be the case.  You could write code like this:

        g = TaskGroup()
        try:
            await g.spawn(coro1)
            await g.spawn(coro2)
            ...
        finally:
            await g.join()
 
    This might be more useful for more persistent or long-lived 
    task groups.

    If any managed task exits with an error, all remaining tasks 
    are cancelled.  The handling of task-related errors takes place when
    the resultof a task group is analyzed.  For example:

        async with TaskGroup(wait=any) as g:
            await g.spawn(coro1)
            await g.spawn(coro2)
            ...
        try:
            result = g.result      # Errors reported here
        except Exception as e:
            print("FAILED:", e)
    '''
    def __init__(self, tasks=(), *, wait=all):
        self._running = set()        # All running tasks
        self._finished = deque()     # All finished tasks
        self._daemonic = set()       # All running daemon tasks
        self._tasks = set()          # Set of all tasks tracked by the group
        self._joined = False
        self._wait = wait            # Wait policy 
        self.completed = None        # First completed task
        for task in tasks:
            assert not task.taskgroup,  "Task already assigned to a task group"
            task.taskgroup = self
            if not task.daemon:
                self._tasks.add(task)
            if task.terminated:
                self._finished.append(task)
            elif task.daemon:
                self._daemonic.add(task)
            else:
                self._running.add(task)

        self._sema = sync.Semaphore(len(self._finished))

    # Property that returns all tracked tasks in task creation order
    @property
    def tasks(self):
        return sorted(self._tasks, key=lambda task: task.id)

    # Property that returns the result of the first completed task
    @property
    def result(self):
        if not self._joined:
            raise RuntimeError("Task group not yet terminated")
        if not self.completed:
            raise RuntimeError("No task successfully completed")
        return self.completed.result

    # Property that returns the exception of the first completed task
    @property
    def exception(self):
        if not self._joined:
            raise RuntimeError("Task group not yet terminated")
        return self.completed.exception if self.completed else None

    # Property that returns all task results (in task creation order)
    @property
    def results(self):
        if not self._joined:
            raise RuntimeError("Task group not yet terminated")
        return [ task.result for task in self.tasks ]

    @property
    def exceptions(self):
        if not self._joined:
            raise RuntimeError("Task group not yet terminated")
        return [ task.exception for task in self.tasks ]
        
    # Triggered on task completion. 
    async def _task_done(self, task):
        if task.daemon:
            self._daemonic.discard(task)
        else:
            self._running.discard(task)
            self._finished.append(task)
            await self._sema.release()

    # Discards a task from the TaskGroup.  Called implicitly if
    # if a task is explicitly joined while under supervision.
    def _task_discard(self, task):
        try:
            self._finished.remove(task)
        except ValueError:
            pass
        self._tasks.discard(task)
        task.taskgroup = None

    async def add_task(self, task):
        '''
        Add an already existing task to the group.
        '''
        if task.taskgroup:
            raise RuntimeError('Task is already part of a task group')

        if self._joined:
            raise RuntimeError("TaskGroup already joined")

        task.taskgroup = self
        if not task.daemon:
            self._tasks.add(task)
        if task.terminated:
            await self._task_done(task)
        elif task.daemon:
            self._daemonic.add(task)
        else:
            self._running.add(task)

    async def spawn(self, coro, *args, daemon=False):
        '''
        Spawn a new task into the task group.
        '''
        if self._joined:
            raise RuntimeError("TaskGroup already joined")
        task = await spawn(coro, *args, daemon=daemon)
        await self.add_task(task)
        return task

    async def spawn_thread(self, func, *args, daemon=False):
        '''
        Spawn a new async thread into a task group
        '''
        from . import thread
        if self._joined:
            raise RuntimeError("TaskGroup already joined")
        thr = await thread.spawn_thread(func, *args, daemon=daemon)
        await self.add_task(thr)
        return thr

    async def next_done(self):
        '''
        Wait for the next task to finish and return it.  This removes it
        from the group.
        '''
        while not self._finished and self._running:
            await self._sema.acquire()

        if self._finished:
            task = self._finished.popleft()
            await task.wait()
            task.taskgroup = None
            self._tasks.discard(task)
        else:
            task = None

        return task

    async def next_result(self):
        '''
        Return the result of the next task that finishes. Note: if task
        terminated via exception, that exception is raised here.
        '''
        task = await self.next_done()
        if task:
            return task.result
        else:
            raise RuntimeError('No task available')

    async def cancel_remaining(self):
        '''
        Cancel all remaining non-daemonic tasks. Tasks are removed
        from the task group when explicitly cancelled.
        '''
        running = list(self._running)
        for task in running:
            await task.cancel(blocking=False)
        for task in running:
            await task.wait()
            self._task_discard(task)

            
    async def join(self):
        '''
        Wait for tasks in a task group to terminate according to the
        wait policy set for the group.  
        '''
        try:
            if self._wait is None:
                # We wait for no-one. Tasks get cancelled on return.
                return

            while self._finished or self._running:
                # If nothing is finished, we wait for something to complete
                while not self._finished:
                    await self._sema.acquire()

                # Examine all currently finished tasks
                while self._finished:
                    task = self._finished.popleft()
                    # Check if it's the first completed task
                    if self.completed is None:
                        # For wait=object, the self.completed attribute is the first non-None result
                        if not ((self._wait is object) and (not task.exception) and (task.result is None)):
                            self.completed = task 

                    # What happens next depends on the wait and error handling policies
                    if task.exception or \
                       (self._wait is any) or \
                       ((self._wait is object) and (task.result is not None)):
                        return

        # Task groups guarantee all tasks cancelled/terminated upon join()
        finally:
            while self._running:
                task = self._running.pop()
                await task.cancel()
            while self._daemonic:
                task = self._daemonic.pop()
                await task.cancel()
            self._joined = True
        return

    async def __aenter__(self):
        return self

    async def __aexit__(self, ty, val, tb):
        if ty:
            await self.cancel_remaining()
        await self.join()

    def __aiter__(self):
        return self

    async def __anext__(self):
        next = await self.next_done()
        if next is None:
            raise StopAsyncIteration
        return next

    # -- Support for use in async threads
    def __enter__(self):
        return thread.AWAIT(self.__aenter__())

    def __exit__(self, *args):
        return thread.AWAIT(self.__aexit__(*args))

    def __iter__(self):
        return thread.AWAIT(self.__aiter__())

    def __next__(self):
        try:
            return thread.AWAIT(self.__anext__())
        except StopAsyncIteration:
            raise StopIteration

# ----------------------------------------------------------------------
# Public-facing task-related functions.  Some of these functions are
# merely a layer over a low-level trap using async/await.  One reason
# for doing this is that the user will get a more proper warning message
# if they use the function without using the required 'await' keyword.
# -----------------------------------------------------------------------

async def current_task():
    '''
    Returns a reference to the current task
    '''
    return await _get_current()

async def spawn(corofunc, *args, daemon=False):
    '''
    Create a new task, running corofunc(*args). Use the daemon=True
    option if the task runs forever as a background task. 
    '''
    coro = meta.instantiate_coroutine(corofunc, *args)
    task = await _spawn(coro)
    task.daemon = daemon
    return task

# Context manager for supervising cancellation masking
class _CancellationManager(object):

    async def __aenter__(self):
        self.task = await current_task()
        self._last_allow_cancel = self.task.allow_cancel
        self.task.allow_cancel = False
        return self

    async def __aexit__(self, ty, val, tb):
        # Restore previous cancellation flag
        self.task.allow_cancel = self._last_allow_cancel

        # If a CancelledError is being raised on exit from a block, it
        # becomes pending in the task if cancellation is not allowed
        # in the outer context.  Curio should never have delivered 
        # such an exception on its own--it could be manually raised however.
        if isinstance(val, CancelledError) and not self.task.allow_cancel:
            self.task.cancel_pending = val
            return True
        else:
            return False

    def __enter__(self):
        return thread.AWAIT(self.__aenter__())

    def __exit__(self, *args):
        return thread.AWAIT(self.__aexit__(*args))

def disable_cancellation(coro=None, *args):
    if coro is None:
        return _CancellationManager()
    else:
        coro = meta.instantiate_coroutine(coro, *args)
        async def run():
            async with _CancellationManager():
                return await coro
        return run()

async def check_cancellation(exc_type=None):
    '''
    Check if there is any kind of pending cancellation. If cancellations
    are currently allowed, and there is a pending exception, it raises the
    exception.  If cancellations are not allowed, it returns the pending
    exception object or None..

    If exc_type is specified, the function checks the type of the specified
    exception against the given type.  If there is a match, the exception
    is returned and cleared.
    '''
    task = await current_task()

    if exc_type and not isinstance(task.cancel_pending, exc_type):
        return None

    if task.cancel_pending and task.allow_cancel:
        try:
            raise task.cancel_pending
        finally:
            task.cancel_pending = None
    else:
        try:
            return task.cancel_pending
        finally:
            if exc_type:
                task.cancel_pending = None

async def set_cancellation(exc):
    '''
    Set a new pending cancellation exception. Returns the old exception.
    '''
    task = await current_task()
    result = task.cancel_pending
    task.cancel_pending = exc
    return result

# Here to avoid circular import issues
from . import thread
from . import sync


