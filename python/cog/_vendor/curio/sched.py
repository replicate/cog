# curio/sched.py
#
# Task-scheduling primitives. These are used to implement low-level
# scheduling operations needed by higher-level abstractions such
# as Events, Locks, Semaphores, and Queues.

__all__ = [ 'SchedFIFO', 'SchedBarrier' ]

# -- Standard Library

from abc import ABC, abstractmethod
from collections import deque

# -- Curio

from .traps import _scheduler_wait, _scheduler_wake


class SchedBase(ABC):

    def __repr__(self):
        return f'{type(self).__name__}<{len(self)} tasks waiting>'

    @abstractmethod
    def __len__(self):
        pass

    @abstractmethod
    def _kernel_suspend(self, task):
        '''
        Suspends a task.  This method *must* return a zero-argument
        callable that removes the just added task from the scheduler
        on cancellation. Called by the kernel.
        '''
        pass

    @abstractmethod
    def _kernel_wake(self, ntasks=1):
        '''
        Wake one or more tasks. Returns a list of the awakened tasks.
        Called by the kernel.
        '''
        pass

    async def suspend(self, reason='SUSPEND'):
        '''
        Suspend the calling task. reason is a string containing
        descriptive text to indicate why (used to set the task state).
        '''
        await _scheduler_wait(self, reason)

    async def wake(self, n=1):
        '''
        Wake one or more suspended tasks.
        '''
        await _scheduler_wake(self, n)


class SchedFIFO(SchedBase):
    '''
    A scheduling FIFO. Tasks sleep and awake in the order of arrival.
    The wake method only awakens a single task. Commonly used to
    implement locks and queues.
    '''
    def __init__(self):
        self._queue = deque()
        self._actual_len = 0

    def __len__(self):
        return self._actual_len

    def _kernel_suspend(self, task):
        # The task is placed inside a 1-item list.  If cancelled, the
        # task is replaced by None, but the list remains on the queue
        # until later pop operations discard it
        item = [task]
        self._queue.append(item)
        self._actual_len += 1

        def remove():
            item[0] = None
            self._actual_len -= 1
        return remove

    def _kernel_wake(self, ntasks=1):
        tasks = []
        while ntasks > 0:
            task, = self._queue.popleft()
            if task:
                tasks.append(task)
                ntasks -= 1
        self._actual_len -= len(tasks)
        return tasks

class SchedBarrier(SchedBase):
    '''
    A scheduling barrier.  Sleeping tasks are collected into a set.
    Waking makes all of the blocked tasks reawaken at the same time.
    Commonly used to implement Event and join().
    '''
    def __init__(self):
        self._tasks = set()

    def __len__(self):
        return len(self._tasks)

    def _kernel_suspend(self, task):
        self._tasks.add(task)
        return lambda: self._tasks.remove(task)

    def _kernel_wake(self, ntasks=1):
        if ntasks == len(self._tasks):
            result = list(self._tasks)
            self._tasks.clear()
        else:
            result = [self._tasks.pop() for _ in range(ntasks)]
        return result

    async def wake(self, n=None):
        '''
        Wake all or a specified number of tasks.
        '''
        n = len(self._tasks) if n is None else n
        await _scheduler_wake(self, n)

