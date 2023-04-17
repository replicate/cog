# curio/debug.py
#
# Task debugging tools

__all__ = [ 'longblock', 'schedtrace', 'traptrace', 'logcrash' ]

import time
import logging
log = logging.getLogger(__name__)

# -- Curio

from .kernel import Activation
from .errors import TaskCancelled

class DebugBase(Activation):
    def __init__(self, *, level=logging.INFO, log=log, filter=None, **kwargs):
        self.level = level
        self.filter = filter
        self.log = log

    def check_filter(self, task):
        if self.filter and task.name not in self.filter:
            return False
        return True

class longblock(DebugBase):
    '''
    Report warnings for tasks that block the event loop for a long duration.
    '''
    def __init__(self, *, max_time=0.05, level=logging.WARNING, **kwargs):
        super().__init__(level=level, **kwargs)
        self.max_time = max_time

    def running(self, task):
        if self.check_filter(task):
            self.start = time.monotonic()

    def suspended(self, task, trap):
        if self.check_filter(task):
            duration = time.monotonic() - self.start
            if duration > self.max_time:
                self.log.log(self.level, '%r ran for %s seconds', task, duration)

class logcrash(DebugBase):
    '''
    Report tasks that crash with an uncaught exception
    '''
    def __init__(self, level=logging.ERROR, **kwargs):
        super().__init__(level=level, **kwargs)

    def suspended(self, task, trap):
        if task.terminated and self.check_filter(task):
            if task.exception and not isinstance(task.exception, (StopIteration, TaskCancelled, KeyboardInterrupt, SystemExit)):
                self.log.log(self.level, '%r crashed', task, exc_info=task.exception)

class schedtrace(DebugBase):
    '''
    Report when tasks run
    '''
    def __init__(self, **kwargs):
        super().__init__(**kwargs)

    def created(self, task):
        if self.check_filter(task):
            self.log.log(self.level, 'CREATE:%f:%r', time.time(), task)

    def running(self, task):
        if self.check_filter(task):
            self.log.log(self.level, 'RUN:%f:%r', time.time(), task)

    def suspended(self, task, trap):
        if self.check_filter(task):
            self.log.log(self.level, 'SUSPEND:%f:%r', time.time(), task)

    def terminated(self, task):
        if self.check_filter(task):
            self.log.log(self.level, 'TERMINATED:%f:%r', time.time(), task)

class traptrace(schedtrace):
    '''
    Report traps executed
    '''
    def suspended(self, task, trap):
        if self.check_filter(task):
            if trap:
                self.log.log(self.level, 'TRAP:%r', trap)
            super().suspended(task, trap)

def _create_debuggers(debug):
    '''
    Create debugger objects.  Called by the kernel to instantiate the objects.
    '''
    if debug is True:
        # Set a default set of debuggers
        debug = [ schedtrace ]

    elif not isinstance(debug, (list, tuple)):
        debug = [ debug ]

    # Create instances
    debug = [ (d() if (isinstance(d, type) and issubclass(d, DebugBase)) else d)
              for d in debug ]
    return debug






