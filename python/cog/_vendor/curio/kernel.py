# curio/kernel.py
#
# Main execution kernel.
#
# Curio is based on a few overarching design principles that drive the code
# you'll find here.
#
# 1. Environmental Isolation.
#
#    Curio strictly separates the environment of async and synchronous
#    programming.  All functionality related to async operation is
#    placed in async-function definitions.  Async functions request
#    the services of the kernel using low-level yield statements
#    (traps).  The kernel is an opaque black-box from the perspective
#    of synchronous code.  There is only one available
#    operation--run(coro) which runs a new task.  There are no other
#    mechanisms available for interacting with the kernel from
#    synchronous code.  A good analogy might be the distinction
#    between user and protected mode in an OS.  User programs run in
#    user-mode and the operating system kernel runs in protected mode.
#    The same thing happens here.  User programs in Curio can only run
#    in async functions. Those programs can request the services of
#    the kernel. However, they're not granted any further access than
#    that (there is no API surface or anything that can be used).
#
# 2. Microkernels
#
#    The low-level kernel is meant to be small, fast, and minimally
#    featureful.  In fact, almost nothing interesting happens in the
#    kernel. Instead, almost every useful part of Curio gets
#    implemented in async functions found elsewhere.  If you're trying
#    to add new features to Curio, don't add them to the kernel. Think
#    about how to create objects and functions that operate at the
#    async-function level instead.  See files such as sync.py or
#    queue.py for examples.
#
# 3. Decoupling
#
#    No part of Curio has direct linkage to the Kernel class (it's
#    not imported or used anywhere else in the code base).   If you want,
#    you can make a completely custom Kernel object and have the
#    rest of Curio run on it.  You just need to make sure you implement
#    the required traps.   This is in contrast to libraries such as
#    asyncio where many parts of the implementation are required to
#    carry a reference to the underlying event loop.

__all__ = [ 'Kernel', 'run' ]

# -- Standard Library

import socket
import time
import os
import errno
from selectors import DefaultSelector, EVENT_READ, EVENT_WRITE
from collections import deque
import threading

# Logger where uncaught exceptions from crashed tasks are logged
import logging
log = logging.getLogger(__name__)

# -- Curio

from .errors import *
from .task import Task
from .traps import _read_wait
from . import meta
from .timequeue import TimeQueue


class Kernel(object):
    '''
    Curio run-time kernel.  The selector argument specifies a
    different I/O selector. The debug argument specifies a list of
    debugger objects to apply. For example:

        from cog._vendor.curio.debug import schedtrace, traptrace
        k = Kernel(debug=[schedtrace, traptrace])

    Use the kernel run() method to submit work to the kernel.
    '''

    def __init__(self, *, selector=None, debug=None, activations=None, taskcls=Task,
                 max_select_timeout=None if os.name != 'nt' else 1.0):

        # Functions to call at shutdown
        self._shutdown_funcs = []

        # I/O Selector setup
        self._selector = selector if selector else DefaultSelector()
        self._call_at_shutdown(self._selector.close)

        # Task table
        self._tasks = {}

        # Coroutine runner function (created upon first call to run())
        self._runner = None

        # Activations
        self._activations = activations if activations else []

        # Debugging (activations in disguise)
        if debug:
            from .debug import _create_debuggers
            self._activations.extend(_create_debuggers(debug))

        # Task creation class
        self._taskcls = taskcls

        self._max_select_timeout = max_select_timeout


    def __del__(self):
        if self._shutdown_funcs is not None:
            raise RuntimeError(
                'Curio kernel not properly terminated.  Please use Kernel.run(shutdown=True)')

    def __enter__(self):
        return self

    def __exit__(self, ty, val, tb):
        if self._shutdown_funcs is not None:
            self.run(shutdown=True)

    def _call_at_shutdown(self, func):
        self._shutdown_funcs.append(func)


    # ----------
    # Submit a new task to the kernel

    def run(self, corofunc=None, *args, shutdown=False):
        if self._shutdown_funcs is None:
            raise RuntimeError("Can't run a kernel that's been shut down or crashed. Create a new kernel.")

        coro = meta.instantiate_coroutine(corofunc, *args) if corofunc else None
        with meta.running(self):
            # Make the kernel runtime environment (if needed)
            if not self._runner:
                self._runner = self._make_kernel_runtime()

            ret_val = ret_exc = None
            # Run the supplied coroutine (if any)
            if coro or not shutdown:
                task = self._runner(coro)
                if task:
                    ret_exc = task.exception
                    ret_val = task.result if not ret_exc else None
                del task

            # If shutdown has been requested, run the shutdown process
            if shutdown:
                # For "reasons" related to task scheduling, the task
                # of shutting down all remaining tasks is best managed
                # by a launching a task dedicated to carrying out the task (sic)
                async def _shutdown_tasks(tocancel):
                    for task in tocancel:
                        await task.cancel()

                tocancel = sorted(self._tasks.values(), key=lambda t: t.id, reverse=True)
                self._runner(_shutdown_tasks(tocancel))
                assert not self._tasks, "New tasks created during shutdown"
                self._runner = None

                # Call registered shutdown functions
                for func in self._shutdown_funcs:
                    func()
                self._shutdown_funcs = None

            if ret_exc:
                raise ret_exc
            else:
                return ret_val

    # ------------------------------------------------------------
    # Kernel runtime
    #
    # This function creates the kernel execution environment. It
    # returns a single function (a closure) that executes a coroutine.
    #
    # At first glance, this function is going to look giant and
    # insane. It is implementing the kernel runtime as a self-contained
    # black box.  There is no external API.  The only possible
    # communication is via traps defined in curio/traps.py.
    # It's best to think of this as a "program within a program".

    def _make_kernel_runtime(kernel):

        # Motto:  "What happens in the kernel stays in the kernel"

        # ---- Kernel State
        current = None                          # Currently running task
        selector = kernel._selector             # Event selector
        ready = deque()                         # Ready queue
        tasks = kernel._tasks                   # Task table
        sleepq = TimeQueue()                    # Sleeping task queue
        wake_queue = deque()                    # Thread wake queue
        _activations = []

        # ---- Bound methods
        selector_register = selector.register
        selector_unregister = selector.unregister
        selector_modify = selector.modify
        selector_select = selector.select
        selector_getkey = selector.get_key
        selector_max_timeout = kernel._max_select_timeout

        ready_popleft = ready.popleft
        ready_append = ready.append
        time_monotonic = time.monotonic
        taskcls = kernel._taskcls

        # ------------------------------------------------------------
        # In-kernel task used for processing futures.
        #
        # Internal task that monitors the loopback socket--allowing the kernel to
        # awake for non-I/O events.

        # Loop-back sockets
        notify_sock = None
        wait_sock = None

        async def _kernel_task():
            wake_queue_popleft = wake_queue.popleft
            while True:
                await _read_wait(wait_sock)
                data = wait_sock.recv(1000)

                # Process any waking tasks.  These are tasks that have
                # been awakened externally to the event loop (e.g., by
                # separate threads, Futures, etc.).
                while wake_queue:
                    task, future = wake_queue_popleft()
                    # If the future associated with wakeup no longer
                    # matches the future stored on the task, wakeup is
                    # abandoned.  It means that a timeout or
                    # cancellation event occurred in the time interval
                    # between the call to wake() and the
                    # subsequent processing of the waking task
                    if future and task.future is not future:
                        continue
                    task.future = None
                    task.state = 'READY'
                    task.cancel_func = None
                    ready_append(task)

        # Force the kernel to wake, possibly scheduling a task to run.
        # This method is called by threads running concurrently to the
        # curio kernel.  For example, it's triggered upon completion of
        # Futures created by thread pools and processes. It's inherently
        # dangerous for any kind of operation on the kernel to be
        # performed by a separate thread.  Thus, the *only* thing that
        # happens here is that the task gets appended to a deque and a
        # notification message is written to the kernel notification
        # socket.  append() and pop() operations on deques are thread safe
        # and do not need additional locking.  See
        # https://docs.python.org/3/library/collections.html#collections.deque
        # ----------
        def wake(task=None, future=None):
            if task:
                wake_queue.append((task, future))

            notify_sock.send(b'\x00')

        def init_loopback():
            nonlocal notify_sock, wait_sock
            notify_sock, wait_sock = socket.socketpair()
            wait_sock.setblocking(False)
            notify_sock.setblocking(False)
            kernel._call_at_shutdown(notify_sock.close)
            kernel._call_at_shutdown(wait_sock.close)

        # ------------------------------------------------------------
        # Task management functions.
        #

        # Create a new task. Putting it on the ready queue
        def new_task(coro):
            task = taskcls(coro, current)
            tasks[task.id] = task
            reschedule_task(task)
            for a in _activations:
                a.created(task)
            return task

        # Reschedule a task, putting it back on the ready queue.
        def reschedule_task(task):
            assert task not in ready

            ready_append(task)
            task.state = 'READY'
            task.cancel_func = None

        # Suspend the current task
        def suspend_task(state, cancel_func):
            nonlocal current
            current.state = state
            current.cancel_func = cancel_func

            # Unregister previous I/O request. Discussion follows:
            #
            # When a task performs I/O, it registers itself with the underlying
            # I/O selector.  When the task is reawakened, it unregisters itself
            # and prepares to run.  However, in many network applications, the
            # task will perform a small amount of work and then go to sleep on
            # exactly the same I/O resource that it was waiting on before. For
            # example, a client handling task in a server will often spend most
            # of its time waiting for incoming data on a single socket.
            #
            # Instead of always unregistering the task from the selector, we
            # can defer the unregistration process until after the task goes
            # back to sleep again.  If it happens to be sleeping on the same
            # resource as before, there's no need to unregister it--it will
            # still be registered from the last I/O operation.
            #
            # The code here performs the unregister step for a task that
            # ran, but is now sleeping for a *different* reason than repeating the
            # prior I/O operation.  There is coordination with code in trap_io().

            if current._last_io:
                unregister_event(*current._last_io)
                current._last_io = None

            current = None

        # Check if task has pending cancellation
        def check_cancellation():
            if current.allow_cancel and current.cancel_pending:
                current._trap_result = current.cancel_pending
                current.cancel_pending = None
                return True
            else:
                return False

        # Set a timeout or sleep event on the current task
        def set_timeout(clock, sleep_type='timeout'):
            if clock is None:
                sleepq.cancel((current.id, sleep_type), getattr(current, sleep_type))
            else:
                sleepq.push((current.id, sleep_type), clock)
            setattr(current, sleep_type, clock)

        # ------------------------------------------------------------
        # I/O Support functions
        #

        def register_event(fileobj, event, task):
            try:
                key = selector_getkey(fileobj)
                mask, (rtask, wtask) = key.events, key.data
                if event == EVENT_READ and rtask:
                    raise ReadResourceBusy(f"Multiple tasks can't wait to read on the same file descriptor {fileobj}")
                if event == EVENT_WRITE and wtask:
                    raise WriteResourceBusy(f"Multiple tasks can't wait to write on the same file descriptor {fileobj}")

                selector_modify(fileobj, mask | event,
                                (task, wtask) if event == EVENT_READ else (rtask, task))
                selector_getkey(fileobj)
            except KeyError:
                selector_register(fileobj, event,
                                  (task, None) if event == EVENT_READ else (None, task))

        def unregister_event(fileobj, event):
            key = selector_getkey(fileobj)
            mask, (rtask, wtask) = key.events, key.data
            mask &= ~event
            if not mask:
                selector_unregister(fileobj)
            else:
                selector_modify(fileobj, mask,
                                (None, wtask) if event == EVENT_READ else (rtask, None))

        # ------------------------------------------------------------
        # Traps
        #
        # These implement the low-level functionality that is
        # triggered by user-level code.  They are never invoked directly
        # and there is no public API outside the kernel.  Instead,
        # coroutines use a statement such as
        #
        #   yield ('trap_io', sock, EVENT_READ, 'READ_WAIT')
        #
        # to invoke a specific trap.
        # ------------------------------------------------------------

        # ----------------------------------------
        # Wait for I/O
        def trap_io(fileobj, event, state):
            if check_cancellation():
                return

            # See comment about deferred unregister in suspend_task(). If the
            # requested I/O operation is *different* than the last I/O operation
            # that was performed by the task, we need to unregister the last I/O
            # resource used and register a new one with the selector.
            if current._last_io != (fileobj, event):
                if current._last_io:
                    unregister_event(*current._last_io)
                try:
                    register_event(fileobj, event, current)
                except CurioError as e:
                    current._trap_result = e
                    return

            # This step indicates that we have managed any deferred I/O management
            # for the task.  Otherwise, I/O will be unregistered.
            current._last_io = None
            suspend_task(state, lambda: unregister_event(fileobj, event))

        # ----------------------------------------
        # Release any kernel resources associated with fileobj.
        def trap_io_release(fileobj):
            if current._last_io:
                unregister_event(*current._last_io)
                current._last_io = None
            current._trap_result = None

        # ----------------------------------------
        # Return tasks currently waiting on a file obj.
        def trap_io_waiting(fileobj):
            try:
                key = selector_getkey(fileobj)
                rtask, wtask = key.data
                rtask = rtask if rtask and rtask.cancel_func else None
                wtask = wtask if wtask and wtask.cancel_func else None
                current._trap_result = (rtask, wtask)
            except KeyError:
                current._trap_result = (None, None)

        # ----------------------------------------
        # Wait on a Future
        def trap_future_wait(future, event):
            if check_cancellation():
                return

            current.future = future

            # Discussion: Each task records the future that it is
            # currently waiting on.  The completion callback below only
            # attempts to wake the task if its stored Future is exactly
            # the same one that was stored above.  Due to support for
            # cancellation and timeouts, it's possible that a task might
            # abandon its attempt to wait for a Future and go on to
            # perform other operations, including waiting for different
            # Future in the future (got it?).  However, a running thread
            # or process still might go on to eventually complete the
            # earlier work.  In that case, it will trigger the callback,
            # find that the task's current Future is now different, and
            # discard the result.

            future.add_done_callback(lambda fut, task=current: wake(task, fut))

            # An optional threading.Event object can be passed and set to
            # start a worker thread.   This makes it possible to have a lock-free
            # Future implementation where worker threads only start after the
            # callback function has been set above.
            if event:
                event.set()

            suspend_task('FUTURE_WAIT',
                          lambda task=current:
                              setattr(task, 'future', future.cancel() and None))

        # ----------------------------------------
        # Add a new task to the kernel
        def trap_spawn(coro):
            task = new_task(coro)
            # task.parentid = current.id
            current._trap_result = task

        # ----------------------------------------
        # Cancel a task
        def trap_cancel_task(task, exc=TaskCancelled, val=None):
            if task.cancelled:
                return

            task.cancelled = True

            # Cancelling a task also cancels any currently pending timeout.
            # If a task is being cancelled, the delivery of a timeout is
            # somewhat immaterial--the task is already being cancelled.
            task.timeout = None

            # Set the cancellation exception
            if isinstance(exc, BaseException):
                task.cancel_pending = exc
            else:
                task.cancel_pending = exc(exc.__name__ if val is None else val)

            # If the task doesn't allow the delivery of a cancellation exception right now
            # we're done.  It's up to the task to check for it later
            if not task.allow_cancel:
                return

            # If the task doesn't have a cancellation function set, it means the task
            # is on the ready-queue.  It's not safe to deliver a cancellation exception
            # to it right now.  Instead, we simply return.  It will get cancelled
            # the next time it performs a blocking operation
            if not task.cancel_func:
                return

            # Cancel and reschedule the task
            task.cancel_func()
            task._trap_result = task.cancel_pending
            reschedule_task(task)
            task.cancel_pending = None

        # ----------------------------------------
        # Wait on a scheduler primitive
        def trap_sched_wait(sched, state):
            if check_cancellation():
                return
            suspend_task(state, sched._kernel_suspend(current))

        # ----------------------------------------
        # Reschedule one or more tasks from a scheduler primitive
        def trap_sched_wake(sched, n):
            tasks = sched._kernel_wake(n)
            for task in tasks:
                reschedule_task(task)

        # ----------------------------------------
        # Return the current value of the kernel clock
        def trap_clock():
            current._trap_result = time_monotonic()

        # ----------------------------------------
        # Sleep for a specified period. Returns value of monotonic clock.
        def trap_sleep(clock):
            nonlocal current
            if check_cancellation():
                return

            if clock <= 0:
                reschedule_task(current)
                current._trap_result = time_monotonic()
                current = None
                return

            set_timeout(clock + time_monotonic(), 'sleep')
            suspend_task('TIME_SLEEP',
                          lambda task=current: (sleepq.cancel((task.id, 'sleep'), task.sleep), setattr(task, 'sleep', None)))

        # ----------------------------------------
        # Set a timeout to be delivered to the calling task
        def trap_set_timeout(timeout):
            old_timeout = current.timeout
            if timeout is None:
                # If no timeout period is given, leave the current timeout in effect
                pass
            else:
                set_timeout(timeout)
                if old_timeout and current.timeout > old_timeout:
                    current.timeout = old_timeout
            current._trap_result = old_timeout

        # ----------------------------------------
        # Clear a previously set timeout
        def trap_unset_timeout(previous):
            # Here's an evil corner case.  Suppose the previous timeout in effect
            # has already expired?  If so, then we need to arrange for a timeout
            # to be generated.  However, this has to happen on the *next* blocking
            # call, not on this trap.  That's because the "unset" timeout feature
            # is usually done in the finalization stage of the previous timeout
            # handling.  If we were to raise a TaskTimeout here, it would get mixed
            # up with the prior timeout handling and all manner of head-explosion
            # will occur.

            set_timeout(None)
            current._trap_result = now = time_monotonic()
            if previous and previous >= 0 and previous < now:
                # Perhaps create a TaskTimeout pending exception here.
                set_timeout(previous)
            else:
                set_timeout(previous)
                current.timeout = previous
                # But there's one other evil corner case.  It's possible that
                # a timeout could be reset while a TaskTimeout exception
                # is pending.  If that happens, it means that the task has
                # left the timeout block.   We should probably take away the
                # pending exception.
                if isinstance(current.cancel_pending, TaskTimeout):
                    current.cancel_pending = None

        # ----------------------------------------
        # Return the running kernel
        def trap_get_kernel():
            current._trap_result = kernel

        # ----------------------------------------
        # Return the currently running task
        def trap_get_current():
            current._trap_result = current

        # ------------------------------------------------------------
        # Final setup.
        # ------------------------------------------------------------

        # Create the traps tables
        kernel._traps = traps = { key:value for key, value in locals().items()
                                  if key.startswith('trap_') }

        # Initialize activations
        kernel._activations = _activations = \
            [ act() if (isinstance(act, type) and issubclass(act, Activation)) else act
                    for act in kernel._activations ]

        for act in _activations:
            act.activate(kernel)

        # Initialize the loopback task (if not already initialized)
        init_loopback()
        task = new_task(_kernel_task())
        task.daemon = True

        # ------------------------------------------------------------
        # Main Kernel Loop.  Runs the supplied coroutine until it
        # terminates. If no coroutine is supplied, it runs one cycle
        # of the kernel.
        # ------------------------------------------------------------
        def kernel_run(coro):
            nonlocal current
            main_task = new_task(coro) if coro else None
            del coro
            trap = None

            while True:
                # ------------------------------------------------------------
                # I/O Polling/Waiting
                # ------------------------------------------------------------

                if ready or not main_task:
                    timeout = 0
                else:
                    current_time = time_monotonic()
                    timeout = sleepq.next_deadline(current_time)
                    if selector_max_timeout and (timeout is None or timeout > selector_max_timeout):
                        timeout = selector_max_timeout
                try:
                    events = selector_select(timeout)
                except OSError as e:
                    # If there is nothing to select, windows throws an
                    # OSError, so just set events to an empty list.
                    wsaeinval = getattr(errno, 'WSAEINVAL', None)
                    einval = getattr(errno, 'EINVAL', None)
                    if e.errno not in (wsaeinval, einval):
                        raise
                    events = []

                # Reschedule tasks with completed I/O
                for key, mask in events:
                    rtask, wtask = key.data
                    emask = key.events
                    intfd = isinstance(key.fileobj, int)
                    if mask & EVENT_READ:
                        # Discussion: If the associated fileobj is *not* a
                        # bare integer file descriptor, we keep a record
                        # of the last I/O event in _last_io and leave the
                        # task registered on the event loop.  If it
                        # performs the same I/O operation again, it will
                        # get a speed boost from not having to re-register
                        # its event. However, it's not safe to use this
                        # optimization with bare integer fds.  These fds
                        # often get reused and there is a possibility that
                        # a fd will get closed and reopened on a different
                        # resource without it being detected by the
                        # kernel.  For that case, its critical that we not
                        # leave the fd on the event loop.
                        rtask._last_io = None if intfd else (key.fileobj, EVENT_READ)
                        reschedule_task(rtask)
                        emask &= ~EVENT_READ
                        rtask = None

                    if mask & EVENT_WRITE:
                        wtask._last_io = None if intfd else (key.fileobj, EVENT_WRITE)
                        reschedule_task(wtask)
                        emask &= ~EVENT_WRITE
                        wtask = None

                    # Unregister the task if fileobj is not an integer fd (see
                    # note above).
                    if intfd:
                        if emask:
                            selector_modify(key.fileobj, emask, (rtask, wtask))
                        else:
                            selector_unregister(key.fileobj)


                # ------------------------------------------------------------
                # Time handling (sleep/timeouts)
                # ------------------------------------------------------------

                current_time = time_monotonic()
                for tm, (taskid, sleep_type) in sleepq.expired(current_time):
                    # When a task wakes, verify that the timeout value matches that stored
                    # on the task. If it differs, it means that the task completed its
                    # operation, was cancelled, or is no longer concerned with this
                    # sleep operation.  In that case, we do nothing
                    task = tasks.get(taskid)

                    if task is None:
                        continue
                    if tm != getattr(task, sleep_type):
                        continue

                    setattr(task, sleep_type, None)

                    if sleep_type == 'sleep':
                        task._trap_result = current_time
                        reschedule_task(task)

                    # If cancellation is allowed and the task is blocked, reschedule it
                    elif task.allow_cancel and task.cancel_func:
                        task.cancel_func()
                        task._trap_result = TaskTimeout(current_time)
                        reschedule_task(task)

                    # Task is on the ready queue or can't be cancelled right now;
                    # mark it as pending cancellation
                    else:
                        task.cancel_pending = TaskTimeout(current_time)

                # ------------------------------------------------------------
                # Run ready tasks
                # ------------------------------------------------------------

                for _ in range(len(ready)):
                    active = current = ready_popleft()
                    for a in _activations:
                        a.running(active)
                    active.state = 'RUNNING'
                    active.cycles += 1

                    # The current task runs until it suspends or terminates
                    while current:
                        try:
                            trap = current.send(current._trap_result)
                        except BaseException as e:
                            # If any exception has occurred, the task is done.
                            current = None

                            # Wake all joining tasks and enter the terminated state.
                            for wtask in active.joining._kernel_wake(len(active.joining)):
                                reschedule_task(wtask)
                            active.terminated = True
                            active.state = 'TERMINATED'
                            del tasks[active.id]
                            active.timeout = None
                            # Normal termination (set the result)
                            if isinstance(e, StopIteration):
                                active.result = e.value
                            else:
                                # Abnormal termination (set an exception)
                                active.exception = e
                                if (active != main_task and not isinstance(e, (CancelledError, SystemExit))):
                                    log.error('Task Crash: %r', active, exc_info=True)
                                if not isinstance(e, (Exception, CancelledError)):
                                    raise
                            break

                        # Run the trap function.  This is never supposed to raise
                        # an exception unless there's a fatal programming error in
                        # the kernel itself.  Such errors cause Curio to die. They
                        # are not reported back to tasks.
                        current._trap_result = None
                        try:
                            traps[trap[0]](*trap[1:])
                        except:
                            # Disable any further use of the kernel on fatal crash.
                            kernel._shutdown_funcs = None
                            raise

                    # --- The active task has suspended

                    # Unregister any prior I/O listening
                    if active._last_io:
                        unregister_event(*active._last_io)
                        active._last_io = None

                    # Trigger scheduler activations (if any)
                    for a in _activations:
                        a.suspended(active, trap)
                        if active.terminated:
                            a.terminated(active)
                    current = active = trap = None

                # If the main task has terminated, we're done.
                if main_task:
                    if main_task.terminated:
                        main_task.joined = True
                        return main_task
                else:
                    return None

        return kernel_run


def run(corofunc, *args, with_monitor=False, selector=None,
        debug=None, activations=None, **kernel_extra):
    '''
    Run the curio kernel with an initial task and execute until all
    tasks terminate.  Returns the task's final result (if any). This
    is a convenience function that should primarily be used for
    launching the top-level task of a curio-based application.  It
    creates an entirely new kernel, runs the given task to completion,
    and concludes by shutting down the kernel, releasing all resources used.

    Don't use this function if you're repeatedly launching a lot of
    new tasks to run in curio. Instead, create a Kernel instance and
    use its run() method instead.
    '''
    kernel = Kernel(selector=selector, debug=debug, activations=activations,
                    **kernel_extra)

    # Check if a monitor has been requested
    if with_monitor or 'CURIOMONITOR' in os.environ:
        from .monitor import Monitor
        m = Monitor(kernel)
        kernel._call_at_shutdown(m.close)
        kernel.run(m.start)

    with kernel:
        return kernel.run(corofunc, *args)

# An Activation is used to monitor and effect what happens
# during task execution in the Curio kernel. They are often used to
# implement tracers, debuggers, and other diagonistic tools.
# See curio/debug.py for some specific examples.

class Activation:

    def activate(self, kernel):
        '''
        Called each time the kernel sets up its environment and is ready to run.
        kernel is an instance of the kernel that's executing.
        '''

    def created(self, task):
        '''
        Called immediately after a task has been created.
        '''

    def running(self, task):
        '''
        Called right before the next execution cycle of a task.
        '''

    def suspended(self, task, trap):
        '''
        Called after the task has suspended due to a trap.
        '''

    def terminated(self, task):
        '''
        Called after a task has terminated, but prior to the task
        being collected by any associated join() operation.
        '''
