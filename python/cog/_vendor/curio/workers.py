# curio/workers.py
#
# Functions for performing work outside of curio.  This includes
# running functions in threads, processes, and executors from the
# concurrent.futures module.

__all__ = ['run_in_executor', 'run_in_thread', 'run_in_process', 'block_in_thread']

# -- Standard Library

import sys
import multiprocessing
import threading
import traceback
import signal
from collections import Counter, defaultdict

# -- Curio

from .errors import CancelledError
from .traps import _future_wait, _get_kernel
from . import sync
from .channel import Connection

# Code to embed a traceback in a remote exception.  This is borrowed
# straight from multiprocessing.pool.  Copied here to avoid possible
# confusion when reading the traceback message (it will identify itself
# as originating from curio as opposed to multiprocessing.pool).

class RemoteTraceback(Exception):

    def __init__(self, tb):
        self.tb = tb

    def __str__(self):
        return self.tb


class ExceptionWithTraceback:

    def __init__(self, exc, tb):
        tb = traceback.format_exception(type(exc), exc, tb)
        tb = ''.join(tb)
        self.exc = exc
        self.tb = '\n"""\n%s"""' % tb

    def __reduce__(self):
        return rebuild_exc, (self.exc, self.tb)


def rebuild_exc(exc, tb):
    exc.__cause__ = RemoteTraceback(tb)
    return exc

async def run_in_executor(exc, callable, *args):
    '''
    Run callable(*args) in an executor such as
    ThreadPoolExecutor or ProcessPoolExecutor from the
    concurrent.futures module.  Be aware that on cancellation, any
    worker thread or process that was handling the request will
    continue to run to completion as a kind of zombie-- possibly
    rendering the executor unusable for subsequent work.

    This function is provided for compatibility with
    concurrent.futures, but is not the recommend approach for running
    blocking or cpu-bound work in curio. Use the run_in_thread() or
    run_in_process() methods instead.
    '''
    future = exc.submit(callable, *args)
    await _future_wait(future)
    return future.result()

MAX_WORKER_THREADS = 64

async def reserve_thread_worker():
    '''
    Reserve a thread pool worker
    '''
    kernel = await _get_kernel()
    if not hasattr(kernel, 'thread_pool'):
        kernel.thread_pool = WorkerPool(ThreadWorker, MAX_WORKER_THREADS)
        kernel._call_at_shutdown(kernel.thread_pool.shutdown)
    return (await kernel.thread_pool.reserve())

async def run_in_thread(callable, *args, call_on_cancel=None):
    '''
    Run callable(*args) in a separate thread and return the result. If
    cancelled, be aware that the requested callable may or may not have
    executed.  If it start running, it will run fully to completion
    as a kind of zombie.
    '''
    worker = None
    try:
        worker = await reserve_thread_worker()
        return await worker.apply(callable, args, call_on_cancel)
    finally:
        if worker:
            await worker.release()

# Support for blocking in threads.
#
# Discussion:
#
# The run_in_thread() function can be used to run any synchronous function
# in a separate thread.  However, certain kinds of operations are
# inherently unsafe.   For example, consider a worker task that wants
# to wait on a threading Event like this:
#
#    evt = threading.Event()     # Foreign Event...
#
#    async def worker():
#        await run_in_thread(evt.wait)
#        print('Alive!')
#
# Now suppose Curio spins up a huge number of workers:
#
#    for n in range(1000):
#        await spawn(worker())
#
# At this point, you're in a bad situation.  The worker tasks have all
# called run_in_thread() and are blocked indefinitely.  Because the
# pool of worker threads is limited, you've exhausted all available
# resources.  Nobody can now call run_in_thread() without blocking.
# There's a pretty good chance that your code is permanently
# deadlocked.  There are dark clouds.
#
# This problem can be solved by wrapping run_in_thread() with a
# semaphore. Like this:
#
#    _barrier = curio.Semaphore()
#
#    async def worker():
#        async with _barrier:
#            await run_in_thread(evt.wait)
#
# However, to make it much more convenient, we can take care of
# a lot of fiddly details.  We can cache the requested callable,
# build a set of semaphores and synchronize things in the background.
# That's what the block_in_thread() function is doing.  For example:
#
#    async def worker():
#        await block_in_thread(evt.wait)
#        print('Alive!')
#
# Unlike run_in_thread(), spawning up 1000 workers creates a
# situation where only 1 worker is actually blocked in a thread.
# The other 999 workers are blocked on a semaphore waiting for service.

_pending = Counter()
_barrier = defaultdict(sync.Semaphore)

async def block_in_thread(callable, *args, call_on_cancel=None):
    '''
    Run callable(*args) in a thread with the expectation that the
    operation is going to block for an indeterminate amount of time.
    Guarantees that at most only one background thread is used
    regardless of how many curio tasks are actually waiting on the
    same callable (e.g., if 1000 Curio tasks all decide to call
    block_on_thread on the same callable, they'll all be handled by a
    single thread). Primary use of this function is on foreign locks,
    queues, and other synchronization primitives where you have to use
    a thread, but you just don't have any idea when the operation will
    complete.
    '''
    if hasattr(callable, '__self__'):
        call_key = (callable.__name__, id(callable.__self__))
    else:
        call_key = id(callable)
    _pending[call_key] += 1
    async with _barrier[call_key]:
        try:
            return await run_in_thread(callable, *args, call_on_cancel=call_on_cancel)
        finally:
            _pending[call_key] -= 1
            if not _pending[call_key]:
                del _pending[call_key]
                del _barrier[call_key]


MAX_WORKER_PROCESSES = multiprocessing.cpu_count()

async def run_in_process(callable, *args):
    '''
    Run callable(*args) in a separate process and return the
    result.  In the event of cancellation, the worker process is
    immediately terminated.

    The worker process is created using multiprocessing.Process().
    Communication with the process uses multiprocessing.Pipe() and an
    asynchronous message passing channel.  All function arguments and
    return values are seralized using the pickle module.  When
    cancelled, the Process.terminate() method is used to kill the
    worker process.  This results in a SIGTERM signal being sent to
    the process.

    The handle_cancellation flag, if True, indicates that you intend
    to manage the worker cancellation yourself.  This an advanced
    option.  Any resulting CancelledError has 'task' and 'worker'
    attributes.  task is a background task that's supervising the
    still executing work.  worker is the associated process.

    The worker process is a separate isolated Python interpreter.
    Nothing should be assumed about its global state including shared
    variables, files, or connections.
    '''
    kernel = await _get_kernel()
    if not hasattr(kernel, 'process_pool'):
        kernel.process_pool = WorkerPool(ProcessWorker, MAX_WORKER_PROCESSES)
        kernel._call_at_shutdown(kernel.process_pool.shutdown)
    worker = None
    try:
        worker = await kernel.process_pool.reserve()
        return await worker.apply(callable, args)
    finally:
        if worker:
            await worker.release()

# The _FutureLess class is a custom "Future" implementation solely for
# use by curio. It is used by the ThreadWorker class below and
# provides only the minimal set of functionality needed to transmit a
# result back to the curio kernel.  Unlike the normal Future class,
# this version doesn't require any thread synchronization or
# notification support.  By eliminating that, the overhead associated
# with the handoff between curio tasks and threads is substantially
# faster.


class _FutureLess(object):
    __slots__ = ('_callback', '_exception', '_result')

    def set_result(self, result):
        self._result = result
        self._callback(self)

    def set_exception(self, exc):
        self._exception = exc
        self._callback(self)

    def result(self):
        try:
            return self._result
        except AttributeError:
            raise self._exception from None

    def add_done_callback(self, func):
        self._callback = func

    def cancel(self):
        pass

# A ThreadWorker represents a thread that performs work on behalf of a
# curio task.   A curio task initiates work by executing the
# apply() method. This passes the request to a background thread that
# executes it.  While this takes place, the curio task blocks, waiting
# for a result to be set on an internal Future.


class ThreadWorker(object):
    '''
    Worker that executes a callable on behalf of a curio task in a separate thread.
    '''

    def __init__(self, pool):
        self.thread = None
        self.start_evt = None
        self.lock = None
        self.request = None
        self.terminated = False
        self.pool = pool

    def _launch(self):
        self.start_evt = threading.Event()
        self.thread = threading.Thread(target=self.run_worker, daemon=True)
        self.thread.start()

    def run_worker(self):
        while True:
            self.start_evt.wait()
            self.start_evt.clear()
            # If there is no pending request, but we were signalled to
            # start, it means terminate.
            if not self.request:
                return

            # Run the request
            self.request()

    async def release(self):
        if self.pool:
            await self.pool.release(self)

    def shutdown(self):
        self.terminated = True
        self.request = None
        if self.start_evt:
            self.start_evt.set()

    async def apply(self, func, args=(), call_on_cancel=None):
        '''
        Run the callable func in a separate thread and return the result.
        '''
        if self.thread is None:
            self._launch()

        # Set up a request for the worker thread
        done_evt = threading.Event()
        done_evt.clear()
        cancelled = False
        future = _FutureLess()

        def run_callable():
            try:
                future.set_result(func(*args))
            except BaseException as err:
                future.set_exception(err)
            finally:
                done_evt.wait()
                if cancelled and call_on_cancel:
                    call_on_cancel(future)

        self.request = run_callable
        try:
            await _future_wait(future, self.start_evt)
            return future.result()
        except CancelledError as e:
            cancelled = True
            self.shutdown()
            raise
        finally:
            done_evt.set()

class ProcessWorker(object):
    '''
    Managed process worker for running CPU-intensive tasks.  The main
    purpose of this class is to run workers with reliable
    cancellation/timeout semantics. Specifically, if a worker is
    cancelled, the underlying process is also killed.   This, as
    opposed to having it linger on running until work is complete.
    '''
    def __init__(self, pool):
        self.process = None
        self.client_ch = None
        self.terminated = False
        self.pool = pool

    def _launch(self):
        context = multiprocessing.get_context('spawn')
        client_ch, server_ch = context.Pipe()
        self.process = context.Process(
            target=self.run_server, args=(server_ch, ), daemon=True)
        self.process.start()
        server_ch.close()
        self.client_ch = Connection.from_Connection(client_ch)

    def shutdown(self):
        self.terminated = True
        if self.process:
            self.process.terminate()
            self.process = None
            self.nrequests = 0

    async def release(self):
        if self.pool:
            await self.pool.release(self)

    def run_server(self, ch):
        signal.signal(signal.SIGTERM, signal.SIG_DFL)
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        while True:
            func, args = ch.recv()
            try:
                result = func(*args)
                ch.send((True, result))
            except Exception as e:
                e = ExceptionWithTraceback(e, e.__traceback__)
                ch.send((False, e))
            del func, args

    async def apply(self, func, args=()):
        if self.process is None or not self.process.is_alive():
            self._launch()

        msg = (func, args)
        try:
            await self.client_ch.send(msg)
            success, result = await self.client_ch.recv()
            if success:
                return result
            else:
                raise result
        except CancelledError:
            self.shutdown()
            raise

# Windows-compatible process worker.  It differs from ProcessWorker in
# that client communication is handled synchronously by a thread.
class WinProcessWorker(ProcessWorker):
    def _launch(self):
        context = multiprocessing.get_context('spawn')
        client_ch, server_ch = context.Pipe()
        self.process = context.Process(
            target=self.run_server, args=(server_ch, ), daemon=True)
        self.process.start()
        server_ch.close()
        self.client_ch = client_ch

    def _client_communicate(self, msg):
        self.client_ch.send(msg)
        return self.client_ch.recv()

    async def apply(self, func, args=()):
        if self.process is None or not self.process.is_alive():
            self._launch()

        msg = (func, args)
        try:
            success, result = await run_in_thread(self._client_communicate, msg)
            if success:
                return result
            else:
                raise result
        except CancelledError:
            self.shutdown()
            raise

if sys.platform.startswith('win'):
    ProcessWorker = WinProcessWorker

# Pool of workers for carrying out jobs on behalf of curio tasks.
#
# This pool works a bit differently than a normal thread/process
# pool due to some of the different ways that threads get used in Curio.
# Instead of submitting work to the pool, you use the reserve() method
# to obtain a worker:
#
#     worker = await pool.reserve()
#
# Once you have a worker, it is yours for as long as you want to have
# it.  To submit work to it, use the apply() method:
#
#     await worker.apply(callable, args)
#
# When you're done with it, release it back to the pool.
#
#     await worker.release()
#
# Some rationale for this design:  Sometimes when you're working with
# threads, you want to perform multiple steps and you need to make sure
# you're performing each step on the same thread for some reason. This
# is especially true if you're trying to manage work cancellation.
# For example, work started in a thread might need to be cleaned up
# on the same thread.  By reserving/releasing workers, we get more
# control over the whole process of how workers get managed.

class WorkerPool(object):

    def __init__(self, workercls, nworkers):
        self.nworkers = sync.Semaphore(nworkers)
        self.workercls = workercls
        self.workers = []

    def shutdown(self):
        for worker in self.workers:
            worker.shutdown()
        self.workers = []

    async def reserve(self):
        await self.nworkers.acquire()
        if not self.workers:
            return self.workercls(self)
        else:
            return self.workers.pop()

    async def release(self, worker):
        if not worker.terminated:
            self.workers.append(worker)
        await self.nworkers.release()


# Pool definitions should anyone want to use them directly
ProcessPool = lambda nworkers: WorkerPool(ProcessWorker, nworkers)
ThreadPool = lambda nworkers: WorkerPool(ThreadWorker, nworkers)
